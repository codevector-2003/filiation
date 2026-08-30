// Package llm generates answers, through either a local Ollama model or a
// user-supplied API key.
//
// A free path must always exist: someone who will not pay for a key still gets
// working answers. With neither Ollama nor a key configured, retrieval still
// returns ranked passages — what is lost is the prose, not the results.
package llm
