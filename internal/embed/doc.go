// Package embed turns text into vectors, over Ollama's local HTTP API.
//
// The interface is deliberately one method, because what sits behind it is
// expected to change (ADR-008). Requiring Ollama is the price of choosing Go; a
// pure-Go ONNX path would restore true one-command setup and is worth
// revisiting after M4.
//
// Ollama being absent is a normal state, not an error. Degrade to keyword
// search and say so — see the degradation ladder in docs/ARCHITECTURE.md.
package embed
