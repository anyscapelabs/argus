// Command server runs argus's agent backend as a plain HTTP service: the
// same storage, scheduler, MCP manager and chat loop the desktop build
// wires through Wails, with REST endpoints instead of the event pump.
//
// This is the second deployment target of the delegation feature: agents
// here run unattended through Chat.RunAgentLoop with the guarded tool
// surface, and integrations connect through the server-side OAuth flow.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/anyscapelabs/argus/Env"
	"github.com/anyscapelabs/argus/Services/Chat"
	"github.com/anyscapelabs/argus/Services/MCP"
	"github.com/anyscapelabs/argus/Services/MCPConnect"
	"github.com/anyscapelabs/argus/Services/Scheduler"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

// defaultModelCaller is the HTTP build's stand-in for a model provider. It
// is a stub on purpose: which provider to call is a deployment decision, and
// a wrong default would silently bill someone's account. Fill this in (wire
// the Router the desktop build uses) before starting the scheduler.
func defaultModelCaller(_ context.Context, _ Chat.LoopConfig, _ []Chat.LoopMessage) (Chat.ModelResponse, error) {
	return Chat.ModelResponse{}, errors.New("no model provider configured for this build; wire one into main.go first")
}

func main() {
	Env.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	core := Shared.NewCore()
	if err := core.Bootstrap(ctx); err != nil {
		log.Fatalf("server: bootstrap failed: %v", err)
	}
	db := core.DB.SQL()

	mcpSvc := &MCP.Service{Core: core}
	if err := mcpSvc.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
		log.Fatalf("server: mcp startup failed: %v", err)
	}

	connect := &MCPConnect.Service{Core: core, MCP: mcpSvc}

	runner := Scheduler.NewAgentRunner(db, mcpSvc.Manager(), defaultModelCaller)
	sched := &Scheduler.Service{Core: core, Chat: runner}
	if err := sched.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
		log.Fatalf("server: scheduler startup failed: %v", err)
	}
	go runner.Pump(ctx)

	mux := http.NewServeMux()
	connect.RegisterRoutes(mux)
	mux.HandleFunc("GET /api/agents", func(w http.ResponseWriter, r *http.Request) {
		agents, err := Storage.ListAgentTeammates(db, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(agents)
	})
	mux.HandleFunc("POST /api/chat/send", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ThreadID string `json:"thread_id"`
			Prompt   string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Prompt == "" {
			http.Error(w, "prompt is required", http.StatusBadRequest)
			return
		}
		if body.ThreadID == "" {
			body.ThreadID = "thr_" + uuid.NewString()
		}
		targetID, _ := Chat.ResolveMentionTarget(db, "default", body.Prompt)
		agentID, err := Chat.HandleUserChatSend(db, "default", body.ThreadID, body.Prompt, runner.IsBusy(targetID), runner.WakeAgent)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"agent_id": agentID, "thread_id": body.ThreadID})
	})

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Printf("server: listening on :8080")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server: %v", err)
	}
}
