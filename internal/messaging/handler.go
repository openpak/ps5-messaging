package messaging

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/openpak/ps5-common/httpx"
	"github.com/openpak/ps5-common/oauth"
)

// Prefix is the path prefix of the API on m.np.playstation.com.
const Prefix = "/api/gamingLoungeGroups/v1"

const (
	defaultLimit = 20
	maxLimit     = 100
	maxBody      = 4000
	typeText     = 1
)

// Handler returns the API mux: /healthz open, everything else behind oauth.Require.
func Handler(st Store, v oauth.Verifier) http.Handler {
	s := &server{st: st}
	api := httpx.Mux()
	api.HandleFunc("GET "+Prefix+"/members/me/groups", s.listGroups)
	api.HandleFunc("POST "+Prefix+"/groups", s.createGroup)
	api.HandleFunc("GET "+Prefix+"/groups/{groupId}", s.getGroup)
	api.HandleFunc("GET "+Prefix+"/members/me/groups/{groupId}", s.getGroup)
	api.HandleFunc("POST "+Prefix+"/groups/{groupId}/threads/{threadId}/messages", s.post)
	api.HandleFunc("GET "+Prefix+"/groups/{groupId}/threads/{threadId}/messages", s.messages)
	api.HandleFunc("GET "+Prefix+"/members/me/groups/{groupId}/threads/{threadId}/messages", s.messages)

	auth := oauth.Require(v)
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { api.ServeHTTP(w, r) })
	root.Handle("/", auth(api))
	return root
}

type server struct{ st Store }

func me(r *http.Request) Member {
	c, _ := oauth.FromContext(r.Context())
	return Member{AccountID: c.AccountID, OnlineID: c.OnlineID}
}

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, 2113410, "Resource not found")
	case errors.Is(err, ErrNotMember):
		httpx.WriteError(w, http.StatusForbidden, 2113411, "Not permitted by access control")
	default:
		httpx.WriteError(w, http.StatusInternalServerError, 2113499, "Internal error")
	}
}

func (s *server) listGroups(w http.ResponseWriter, r *http.Request) {
	gs := s.st.GroupsOf(me(r).AccountID)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"groups": gs, "totalItemCount": len(gs)})
}

func (s *server) createGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupName string   `json:"groupName"`
		Invitees  []Member `json:"invitees"`
	}
	if err := httpx.ReadJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, 2113401, "Invalid request body")
		return
	}
	g, err := s.st.CreateGroup(me(r), req.Invitees, req.GroupName)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, 2113402, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"groupId": g.GroupID, "mainThread": map[string]string{"threadId": g.MainThread.ThreadID}})
}

func (s *server) getGroup(w http.ResponseWriter, r *http.Request) {
	g, err := s.st.Group(r.PathValue("groupId"), me(r).AccountID)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, g)
}

func (s *server) post(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MessageType int    `json:"messageType"`
		Body        string `json:"body"`
	}
	if err := httpx.ReadJSON(r, &req); err != nil || req.Body == "" || len(req.Body) > maxBody {
		httpx.WriteError(w, http.StatusBadRequest, 2113401, "Invalid message")
		return
	}
	if req.MessageType == 0 {
		req.MessageType = typeText
	}
	if req.MessageType != typeText {
		httpx.WriteError(w, http.StatusBadRequest, 2113403, "Only text messages (messageType 1) are supported")
		return
	}
	m, err := s.st.Post(r.PathValue("groupId"), r.PathValue("threadId"), me(r), req.MessageType, req.Body)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"messageUid": m.MessageUID, "createdTimestamp": m.CreatedTimestamp})
}

func (s *server) messages(w http.ResponseWriter, r *http.Request) {
	limit := defaultLimit
	if q := r.URL.Query().Get("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 {
			httpx.WriteError(w, http.StatusBadRequest, 2113401, "Invalid limit")
			return
		}
		limit = min(n, maxLimit)
	}
	ms, total, err := s.st.Messages(r.PathValue("groupId"), r.PathValue("threadId"), me(r).AccountID, limit)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"messages": ms, "messageCount": len(ms), "reachedEndOfPage": len(ms) >= total,
		"previous": "", "next": "",
	})
}
