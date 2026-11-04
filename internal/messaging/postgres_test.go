package messaging

import (
	"context"
	"os"
	"testing"

	"github.com/openpak/ps5-common/pgdb"
)

func openPG(t *testing.T) (*Postgres, string) {
	t.Helper()
	url := os.Getenv("PS5_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PS5_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgdb.Open(ctx, url, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	st, err := NewPostgres(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE ps5_msg_messages, ps5_msg_members, ps5_msg_groups RESTART IDENTITY`); err != nil {
		t.Fatal(err)
	}
	return st, url
}

func TestPostgresMessagesSurviveRestart(t *testing.T) {
	st, url := openPG(t)
	g, err := st.CreateGroup(Member{AccountID: "1", OnlineID: "Alice"}, []Member{{AccountID: "2"}}, "")
	if err != nil || g.GroupType != 0 || g.GroupID != "g1" || g.MainThread.ThreadID != "t1" {
		t.Fatal(g, err)
	}
	if _, err := st.CreateGroup(Member{AccountID: "1"}, nil, ""); err == nil {
		t.Fatal("group without invitee accepted")
	}
	if _, err := st.Post(g.GroupID, g.MainThread.ThreadID, Member{AccountID: "3"}, 1, "x"); err != ErrNotMember {
		t.Fatal(err)
	}
	if _, err := st.Post(g.GroupID, "t9", Member{AccountID: "1"}, 1, "x"); err != ErrNotFound {
		t.Fatal(err)
	}
	st.Post(g.GroupID, g.MainThread.ThreadID, Member{AccountID: "1", OnlineID: "Alice"}, 1, "hello")
	st.Post(g.GroupID, g.MainThread.ThreadID, Member{AccountID: "2", OnlineID: "Bob"}, 1, "hi back")

	ctx := context.Background()
	pool, err := pgdb.Open(ctx, url, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	st2, err := NewPostgres(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	gs := st2.GroupsOf("2")
	if len(gs) != 1 || gs[0].MainThread.LatestMessage == nil || gs[0].MainThread.LatestMessage.Body != "hi back" {
		t.Fatalf("%+v", gs)
	}
	if gs[0].Members[1].OnlineID != "Bob" || gs[0].Members[0].OnlineID != "Alice" {
		t.Fatalf("%+v", gs[0].Members)
	}
	ms, total, err := st2.Messages("g1", "t1", "1", 10)
	if err != nil || total != 2 || len(ms) != 2 || ms[0].Body != "hi back" || ms[1].Body != "hello" {
		t.Fatal(ms, total, err)
	}
	if ms, _, _ := st2.Messages("g1", "t1", "1", 1); len(ms) != 1 {
		t.Fatal("limit ignored")
	}
	if _, _, err := st2.Messages("g1", "t1", "3", 10); err != ErrNotMember {
		t.Fatal(err)
	}
	if _, err := st2.Group("g99", "1"); err != ErrNotFound {
		t.Fatal(err)
	}
	if len(st2.GroupsOf("9")) != 0 {
		t.Fatal("stranger sees groups")
	}
}

func TestPostgresErase(t *testing.T) {
	st, _ := openPG(t)
	dm, _ := st.CreateGroup(Member{AccountID: "1"}, []Member{{AccountID: "2"}}, "")
	st.Post(dm.GroupID, dm.MainThread.ThreadID, Member{AccountID: "2"}, 1, "hi")
	grp, _ := st.CreateGroup(Member{AccountID: "1"}, []Member{{AccountID: "2"}, {AccountID: "3"}}, "g")
	st.Post(grp.GroupID, grp.MainThread.ThreadID, Member{AccountID: "1"}, 1, "from 1")
	st.Post(grp.GroupID, grp.MainThread.ThreadID, Member{AccountID: "2"}, 1, "from 2")
	n, err := st.Erase([]string{"1"})
	if err != nil || n != 6 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if len(st.GroupsOf("1")) != 0 || len(st.GroupsOf("2")) != 1 {
		t.Fatal("wrong groups left")
	}
	if _, total, _ := st.Messages(grp.GroupID, grp.MainThread.ThreadID, "2", 10); total != 1 {
		t.Fatalf("total %d", total)
	}
	if n, err := st.Erase([]string{"1"}); err != nil || n != 0 {
		t.Fatalf("second run n=%d err=%v", n, err)
	}
}
