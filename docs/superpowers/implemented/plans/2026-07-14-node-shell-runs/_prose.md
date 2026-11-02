# Node-session classification and attach hardening

The source spec's locked Q&A is the decision record. The local tmux gateway
must distinguish `node:<name>` login shells from run sessions, preserve node
identity across listing and exact attach, and enforce reconnect/lifecycle and
multi-node safety at the gateway boundary. This is the hardening leg, not a
one-line prefix filter.

Acceptance: node-interactive-shell, node-shell-owner-only. No manual phase.
