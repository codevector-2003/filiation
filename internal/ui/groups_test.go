package ui

import (
	"net/http"
	"testing"
)

func TestCitationGroupsAnswer(t *testing.T) {
	t.Parallel()
	h, id, _ := openTest(t)

	type groups struct {
		Groups []groupJSON `json:"groups"`
		Detail string      `json:"detail"`
		Total  int         `json:"total"`
		Linked int         `json:"linked"`
	}
	got := decode[groups](t, ask(t, h, "GET", "/api/groups?detail=fine"))
	if got.Groups == nil || len(got.Groups) != 0 || got.Detail != "fine" || got.Total != 1 || got.Linked != 0 {
		t.Fatalf("a library with no citations = %+v", got)
	}
	if w := ask(t, h, "GET", "/api/groups?detail=tiny"); w.Code != http.StatusBadRequest {
		t.Errorf("an unknown detail = %d", w.Code)
	}
	if w := ask(t, h, "GET", "/api/papers?group="+id); w.Code != http.StatusNotFound {
		t.Errorf("the Papers list kept to a group not found = %d", w.Code)
	}
	if w := ask(t, h, "GET", "/api/papers/ids?group=Around+nobody"); w.Code != http.StatusNotFound {
		t.Errorf("the map's papers kept to a group not found = %d", w.Code)
	}
	if w := post(t, h, "/api/groups/"+id+"/collection", `{"name":"Bees"}`); w.Code != http.StatusNotFound {
		t.Errorf("a collection from a group not found = %d", w.Code)
	}
	if w := post(t, h, "/api/groups/F00000000/dismiss", `{"dismissed":true}`); w.Code != http.StatusNotFound {
		t.Errorf("dismissing a paper not here = %d", w.Code)
	}
	if w := post(t, h, "/api/groups/"+id+"/dismiss", `{"dismissed":true}`); w.Code != http.StatusNoContent {
		t.Errorf("dismissing = %d %s", w.Code, w.Body)
	}
}
