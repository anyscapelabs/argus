package Storage

import "testing"

func TestMigration15ImportsLegacyMemoryIntoDefaultAgent(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.conn.Exec(
		`INSERT INTO user_memories (key, value, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"timezone", "Europe/London", 10, 20,
	); err != nil {
		t.Fatal(err)
	}
	if err := migration15(db); err != nil {
		t.Fatal(err)
	}

	if _, found, err := db.GetAgentProfile("default"); err != nil || !found {
		t.Fatalf("default profile was not created for legacy data: found=%v err=%v", found, err)
	}
	memory, found, err := db.GetMemory("default", "timezone")
	if err != nil || !found {
		t.Fatalf("legacy memory was not imported: found=%v err=%v", found, err)
	}
	if memory.ID != "legacy:default:timezone" || memory.Value != "Europe/London" || memory.Scope != "always" || memory.SourceType != "legacy" {
		t.Fatalf("legacy memory was imported incorrectly: %+v", memory)
	}
}

func TestMigration18CreatesSkillAvoidAndLessonTables(t *testing.T) {
	db := openTestDB(t)
	for i := 1; i <= 18; i++ {
		switch i {
		case 1:
			if err := migration1(db); err != nil {
				t.Fatal(err)
			}
		case 2:
			if err := migration2(db); err != nil {
				t.Fatal(err)
			}
		case 3:
			if err := migration3(db); err != nil {
				t.Fatal(err)
			}
		case 4:
			if err := migration4(db); err != nil {
				t.Fatal(err)
			}
		case 5:
			if err := migration5(db); err != nil {
				t.Fatal(err)
			}
		case 6:
			if err := migration6(db); err != nil {
				t.Fatal(err)
			}
		case 7:
			if err := migration7(db); err != nil {
				t.Fatal(err)
			}
		case 8:
			if err := migration8(db); err != nil {
				t.Fatal(err)
			}
		case 9:
			if err := migration9(db); err != nil {
				t.Fatal(err)
			}
		case 10:
			if err := migration10(db); err != nil {
				t.Fatal(err)
			}
		case 11:
			if err := migration11(db); err != nil {
				t.Fatal(err)
			}
		case 12:
			if err := migration12(db); err != nil {
				t.Fatal(err)
			}
		case 13:
			if err := migration13(db); err != nil {
				t.Fatal(err)
			}
		case 14:
			if err := migration14(db); err != nil {
				t.Fatal(err)
			}
		case 15:
			if err := migration15(db); err != nil {
				t.Fatal(err)
			}
		case 16:
			if err := migration16(db); err != nil {
				t.Fatal(err)
			}
		case 17:
			if err := migration17(db); err != nil {
				t.Fatal(err)
			}
		case 18:
			if err := migration18(db); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, table := range []string{"agent_skills", "learned_tool_lessons", "memory_avoid_rules"} {
		var name string
		if err := db.conn.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("table %s missing after migration18: %v", table, err)
		}
	}
	if err := db.SaveAgentProfile(AgentProfile{ID: "default", AgentName: "Argus"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetAgentSkillState("default", "gmail", true); err != nil {
		t.Fatal(err)
	}
	if enabled, found, err := db.GetAgentSkillState("default", "gmail"); err != nil || !found || !enabled {
		t.Fatalf("skill state not persisted: found=%v enabled=%v err=%v", found, enabled, err)
	}
	if err := db.UpsertToolLesson("default", "gmail", "gmail__send_message", "use threadId"); err != nil {
		t.Fatal(err)
	}
	if lessons, err := db.ListToolLessons("default", []string{"gmail"}, 5); err != nil || len(lessons) == 0 {
		t.Fatalf("tool lesson not stored/listed: lessons=%v err=%v", lessons, err)
	}
	if _, err := db.UpsertMemory(Memory{
		AgentID: "default", Key: "no_emojis", Value: "Do not use emojis.", Scope: "avoid", SourceType: "explicit",
	}); err != nil {
		t.Fatal(err)
	}
	avoids, err := db.ListAvoidMemories("default", 10)
	if err != nil || len(avoids) != 1 || avoids[0].Scope != "avoid" {
		t.Fatalf("avoid memory not stored correctly: avoids=%+v err=%v", avoids, err)
	}
}
