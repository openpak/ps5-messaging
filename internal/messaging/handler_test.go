package messaging

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openpak/ps5-common/oauth"
)

func setup(t *testing.T) (http.Handler, func(acct, oid string) string) {
	t.Helper()
	sg, err := oauth.NewSigner([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	tok := func(acct, oid string) string {
		s, err := sg.Sign(oauth.Claims{AccountID: acct, OnlineID: oid, Expires: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	return Handler(NewMemory(), sg), tok
}

func do(h http.Handler, method, path, token, body string) (int, map[string]any) {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w.Code, m
}

func TestGroupFlow(t *testing.T) {
	h, tok := setup(t)
	alice, bob, eve := tok("1", "alice"), tok("2", "bob"), tok("3", "eve")

	if c, _ := do(h, "GET", Prefix+"/members/me/groups", "", ""); c != 401 {
		t.Fatalf("no token: %d", c)
	}
	c, g := do(h, "POST", Prefix+"/groups", alice, `{"invitees":[{"accountId":"2"}]}`)
	if c != 200 || g["groupId"] == nil {
		t.Fatalf("create: %d %v", c, g)
	}
	gid := g["groupId"].(string)
	tid := g["mainThread"].(map[string]any)["threadId"].(string)
	base := Prefix + "/groups/" + gid + "/threads/" + tid + "/messages"

	c, p := do(h, "POST", base, alice, `{"messageType":1,"body":"hello"}`)
	if c != 200 || p["messageUid"] == nil || p["createdTimestamp"] == nil {
		t.Fatalf("post: %d %v", c, p)
	}
	do(h, "POST", base, bob, `{"messageType":1,"body":"hi"}`)

	c, l := do(h, "GET", Prefix+"/members/me/groups/"+gid+"/threads/"+tid+"/messages?limit=10", bob, "")
	ms, _ := l["messages"].([]any)
	if c != 200 || len(ms) != 2 || l["reachedEndOfPage"] != true {
		t.Fatalf("list: %d %v", c, l)
	}
	first := ms[0].(map[string]any)
	if first["body"] != "hi" || first["sender"].(map[string]any)["onlineId"] != "bob" {
		t.Fatalf("order/sender: %v", first)
	}
	_, l = do(h, "GET", base+"?limit=1", alice, "")
	if l["reachedEndOfPage"] != false || len(l["messages"].([]any)) != 1 {
		t.Fatalf("limit: %v", l)
	}

	_, gl := do(h, "GET", Prefix+"/members/me/groups", bob, "")
	gs := gl["groups"].([]any)
	if len(gs) != 1 || gs[0].(map[string]any)["mainThread"].(map[string]any)["latestMessage"] == nil {
		t.Fatalf("groups: %v", gl)
	}
	if _, el := do(h, "GET", Prefix+"/members/me/groups", eve, ""); len(el["groups"].([]any)) != 0 {
		t.Fatalf("eve sees groups: %v", el)
	}
	if c, _ := do(h, "GET", base, eve, ""); c != 403 {
		t.Fatalf("non-member read: %d", c)
	}
	if c, _ := do(h, "POST", base, eve, `{"body":"x"}`); c != 403 {
		t.Fatalf("non-member post: %d", c)
	}
	if c, _ := do(h, "POST", Prefix+"/groups/nope/threads/x/messages", alice, `{"body":"x"}`); c != 404 {
		t.Fatalf("unknown group: %d", c)
	}
	if c, _ := do(h, "POST", Prefix+"/groups", alice, `{"invitees":[]}`); c != 400 {
		t.Fatalf("empty group: %d", c)
	}
	if c, _ := do(h, "GET", "/healthz", "", ""); c != 200 {
		t.Fatalf("healthz: %d", c)
	}
}

func TestMemoryErase(t *testing.T) {
	m := NewMemory()
	dm, _ := m.CreateGroup(Member{AccountID: "1"}, []Member{{AccountID: "2"}}, "")
	m.Post(dm.GroupID, dm.MainThread.ThreadID, Member{AccountID: "2"}, 1, "hi")
	grp, _ := m.CreateGroup(Member{AccountID: "1"}, []Member{{AccountID: "2"}, {AccountID: "3"}}, "g")
	m.Post(grp.GroupID, grp.MainThread.ThreadID, Member{AccountID: "1"}, 1, "from 1")
	m.Post(grp.GroupID, grp.MainThread.ThreadID, Member{AccountID: "2"}, 1, "from 2")
	other, _ := m.CreateGroup(Member{AccountID: "2"}, []Member{{AccountID: "3"}}, "")
	m.Post(other.GroupID, other.MainThread.ThreadID, Member{AccountID: "3"}, 1, "kept")

	// dm: group + 2 members + 1 message = 4; grp: 1 member + 1 message = 2.
	n, err := m.Erase([]string{"1"})
	if err != nil || n != 6 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if len(m.GroupsOf("1")) != 0 {
		t.Fatal("account still in a group")
	}
	if _, err := m.Group(dm.GroupID, "2"); err != ErrNotFound {
		t.Fatalf("direct message survived: %v", err)
	}
	msgs, total, err := m.Messages(grp.GroupID, grp.MainThread.ThreadID, "2", 10)
	if err != nil || total != 1 || msgs[0].Body != "from 2" {
		t.Fatal(msgs, total, err)
	}
	if _, total, _ := m.Messages(other.GroupID, other.MainThread.ThreadID, "2", 10); total != 1 {
		t.Fatal("unrelated group touched")
	}
	if n, err := m.Erase([]string{"1"}); err != nil || n != 0 {
		t.Fatalf("second run n=%d err=%v", n, err)
	}
}
