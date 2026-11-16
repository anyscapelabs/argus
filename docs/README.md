# Argus documentation

This folder contains the main documentation for the Argus desktop app. Start here if you want to understand how the app works, where data is stored, or how to work on the codebase.

## Index

- [Architecture overview](architecture.md)  
  How the app is put together: Wails backend, React frontend, service wiring, startup, event flow, and the main architectural decisions.

- [Security](security.md)  
  How Argus handles secrets, pairing, permissions, account isolation, MCP access, browser automation safety, and local data boundaries.

- [Data and storage](data-storage.md)  
  What Argus stores in SQLite, the OS keyring, on disk, and in memory; schema highlights, file locations, caching, and account data wipe behavior.

- [Services and features](services-and-features.md)  
  What each major part of Argus does: chat and agent runs, tools, permissions, MCP/integrations, browser automation, scheduler, artifacts, uploads, voice, skills, and memory.

- [Development guide](development.md)  
  Local setup, dev/build/test commands, configuration, bindings, and project layout.

## Deeper subsystem docs

- [Chat pipeline](architecture/chat-pipeline.md)  
  Stage-by-stage behavior for a chat turn: prompt, transport, agent loop, tools, persistence, and presentation.

- [Gateway architecture](architecture/gateway/README.md)
  - [Providers](architecture/gateway/providers.md)
  - [Models](architecture/gateway/models.md)
  - [Catalog](architecture/gateway/catalog.md)

## Scope

These docs describe the codebase as it exists in this repository. If you change behavior, storage schema, service contracts, or security-sensitive flows, update the relevant document in the same change.
