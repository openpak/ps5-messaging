// Package erase is the account-erasure plumbing every ps5 service shares (card #120): the
// bearer-guarded DELETE /internal/accounts/{account_id}/data endpoint and the resolution of
// the ids an account holds on this network.
package erase

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/openpak/ps5-common/accountcore"
	"github.com/openpak/ps5-common/httpx"
)

// Func erases everything the service holds for the ids and reports the rows removed.
// It must be idempotent.
type Func func(ctx context.Context, ids []string) (int, error)

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IDs returns every id the account may be stored under on this network: the core account id
// and, when the core has an active ps5 link for it, the link subject. core may be nil (no
// core configured): then only the account id is returned. A core error other than "no link"
// fails the erase rather than silently skipping data.
func IDs(ctx context.Context, core accountcore.Core, accountID string) ([]string, error) {
	ids := []string{accountID}
	if core == nil {
		return ids, nil
	}
	l, err := core.GetActiveLinkByAccount(ctx, accountcore.Namespace, accountID)
	if err != nil {
		if accountcore.IsNotFound(err) {
			return ids, nil
		}
		return nil, err
	}
	if l != nil && l.SubjectID != "" && l.SubjectID != accountID {
		ids = append(ids, l.SubjectID)
	}
	return ids, nil
}

// Handler serves DELETE /internal/accounts/{account_id}/data for service. key is
// ACCOUNT_ERASE_KEY: empty answers 503 (never open). On success it logs one `account erased`
// line and answers {"service":...,"rows":N}.
func Handler(service, key string, core accountcore.Core, fn Func, log *slog.Logger) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /internal/accounts/{account_id}/data", func(w http.ResponseWriter, r *http.Request) {
		if key == "" {
			http.Error(w, "erase disabled", http.StatusServiceUnavailable)
			return
		}
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		id := r.PathValue("account_id")
		if !uuidRE.MatchString(id) {
			http.Error(w, "invalid account id", http.StatusBadRequest)
			return
		}
		id = strings.ToLower(id)
		ids, err := IDs(r.Context(), core, id)
		var n int
		if err == nil {
			n, err = fn(r.Context(), ids)
		}
		if err != nil {
			log.Error("account erase failed", "service", service, "error", err)
			http.Error(w, "erase failed", http.StatusInternalServerError)
			return
		}
		log.Info("account erased", "account", id, "rows", n, "service", service)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"service": service, "rows": n})
	})
	return mux
}

// Serve runs the erase listener on addr (ERASE_ADDR, default ":9099") until ctx ends. With an
// empty key nothing is served. It is meant to run in its own goroutine.
func Serve(ctx context.Context, addr, key string, h http.Handler, log *slog.Logger) {
	if key == "" {
		log.Warn("ACCOUNT_ERASE_KEY unset: account erasure endpoint not served")
		return
	}
	if addr == "" {
		addr = ":9099"
	}
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("erase listener stopped", "err", err)
	}
}
