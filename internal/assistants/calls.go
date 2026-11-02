package assistants

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// callLogMax is how large the log may grow before it starts again.
const callLogMax = 1 << 20

// CallLogName is the log's file, in the cache folder. It stays on this computer.
const CallLogName = "assistant-calls.jsonl"

// Call is one tool an assistant called.
type Call struct {
	At     time.Time `json:"at"`
	Tool   string    `json:"tool"`
	Args   string    `json:"args,omitempty"`
	Failed bool      `json:"failed,omitempty"`
}

// Record adds a call to the log at path. A failure costs only the log, never
// the assistant's call, so it is not reported.
func Record(path string, c Call) {
	if path == "" {
		return
	}
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() > callLogMax {
		os.Remove(path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(append(b, '\n'))
}

// Recent returns the last n calls in the log at path, newest first. A line
// that cannot be read, as a half-written one cannot, is passed over.
func Recent(path string, n int) []Call {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var all []Call
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), callLogMax)
	for sc.Scan() {
		var c Call
		if json.Unmarshal(sc.Bytes(), &c) == nil && c.Tool != "" {
			all = append(all, c)
		}
	}
	out := make([]Call, 0, min(n, len(all)))
	for i := len(all) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, all[i])
	}
	return out
}
