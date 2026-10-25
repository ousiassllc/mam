// Package mcp holds the handlers of the "MCP サーバー" category of the global settings screen
// (spec/api/endpoints-settings.md §2–§3, spec/components/settings-screen-categories.md §4): the modals that change the
// address the server listens on and issue a token, their saves, and the revoke of a token. It is a package of its own
// because handler is at its size limit; like handler, it depends on the service layer and the web layer only.
package mcp

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/mcpsettings"
	"github.com/ousiassllc/mam/backend/internal/service/mcptoken"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/mcp"
)

// Handlers answer the routes of the category (router/settingsroutes registers them).
type Handlers struct {
	// Svc is the listener and the tokens.
	Svc *mcpsettings.Service
	// Page answers a request for a modal that is not HTMX (an address typed in, a reload) with the page of the category.
	Page gin.HandlerFunc
}

// SectionData is the category as the settings screen shows it (the page handler of the settings screen calls it). When
// the tokens cannot be read it still shows the listener, with the reason as the error.
func SectionData(ctx context.Context, svc *mcpsettings.Service) view.Data {
	v, err := svc.View(ctx)
	d := dataOf(v)
	if err != nil {
		d.Error = "トークンの一覧を読み込めませんでした。mam のログを確認してください"
	}
	return d
}

func dataOf(v mcpsettings.View) view.Data {
	d := view.Data{
		Addr: v.Addr, Saved: v.Saved, Problem: v.Problem, Candidates: v.Candidates,
		MaxNameLen: mcptoken.MaxNameRunes,
	}
	for _, t := range v.Tokens {
		d.Tokens = append(d.Tokens, rowOf(t))
	}
	return d
}

func rowOf(t mcptoken.Token) view.TokenRow {
	return view.TokenRow{ID: t.ID, Name: t.Name, Prefix: t.Prefix, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt, Revoked: t.Revoked()}
}

// EditBind answers GET /settings/mcp/bind/edit with the modal that changes the bind address.
func (h Handlers) EditBind(c *gin.Context) {
	if !settingshttp.IsHTMX(c) {
		h.Page(c)
		return
	}
	settingshttp.NoStore(c)
	d := SectionData(c.Request.Context(), h.Svc)
	settingshttp.Render(c, http.StatusOK, view.BindModal(view.BindForm{Address: d.Saved, Candidates: d.Candidates}))
}

// NewToken answers GET /settings/mcp/tokens/new with the modal that issues a token.
func (h Handlers) NewToken(c *gin.Context) {
	if !settingshttp.IsHTMX(c) {
		h.Page(c)
		return
	}
	settingshttp.NoStore(c)
	settingshttp.Render(c, http.StatusOK, view.IssueModal(view.IssueForm{MaxNameLen: mcptoken.MaxNameRunes}))
}

// Bind moves the MCP listener to the address of the modal's form (PUT /settings/mcp/bind, field bind_address). A switch
// that worked answers with the card of the address replaced out of band, a toast and the event that closes the modal; one
// that did not is 422 with the reason when the address is not allowed, and 500 when the listener could not be opened or
// the setting saved (the old listener keeps serving either way): the modal again, with the reason and what was typed.
func (h Handlers) Bind(c *gin.Context) {
	addr := strings.TrimSpace(c.PostForm("bind_address"))
	err := h.Svc.Rebind(addr)
	d := SectionData(c.Request.Context(), h.Svc)
	var rejected *mcpsettings.RejectedError
	switch {
	case err == nil:
		settingshttp.CloseModal(c)
		settingshttp.Render(c, http.StatusOK, templ.Join(view.BindRow(d, true), atoms.SuccessToastOOB("待ち受け先を "+addr+" に切り替えました。接続し直してください")))
	case errors.As(err, &rejected):
		settingshttp.Render(c, http.StatusUnprocessableEntity, view.BindModal(view.BindForm{Address: addr, Candidates: d.Candidates, Error: rejected.Message}))
	default:
		_ = c.Error(err)
		settingshttp.Render(c, http.StatusInternalServerError, view.BindModal(view.BindForm{
			Address: addr, Candidates: d.Candidates,
			Error: "待ち受け先を切り替えられませんでした（元の待ち受け先のままです）。ポートの空きと mam のログを確認してください",
		}))
	}
}

// Issue issues a token named by the modal's form (POST /settings/mcp/tokens, field name). It is the one answer that holds
// the value, which is shown this once: 201, the body of the modal replaced by the issued token and a copy button (the
// modal stays open), and the row of the token added to the table out of band, without its value. The answer is not
// cacheable: the value must not stay in a cache or the history. A name that is refused is 422 and the modal again, with
// the reason and what was typed.
func (h Handlers) Issue(c *gin.Context) {
	settingshttp.NoStore(c)
	name := c.PostForm("name")
	issued, err := h.Svc.Tokens.Issue(c.Request.Context(), name)
	var invalid *mcptoken.InvalidError
	switch {
	case err == nil:
		settingshttp.Render(c, http.StatusCreated, templ.Join(
			view.TokenAppended(rowOf(issued.Token)),
			view.IssuedModal(view.NewToken{Name: issued.Token.Name, Value: issued.Value}),
		))
	case errors.As(err, &invalid):
		settingshttp.Render(c, http.StatusUnprocessableEntity, view.IssueModal(view.IssueForm{Name: name, MaxNameLen: mcptoken.MaxNameRunes, Error: invalid.Message}))
	default:
		_ = c.Error(err)
		settingshttp.Render(c, http.StatusInternalServerError, view.IssueModal(view.IssueForm{
			Name: name, MaxNameLen: mcptoken.MaxNameRunes, Error: "トークンを発行できませんでした。mam のログを確認してください",
		}))
	}
}

// Revoke ends the token (DELETE /settings/mcp/tokens/:id) and answers with its row (a revoked token stays listed, with its
// state) and a toast. A token that does not exist is 404 as a toast: the table stays as it is.
func (h Handlers) Revoke(c *gin.Context) {
	id := c.Param("id")
	err := h.Svc.Tokens.Revoke(c.Request.Context(), id)
	switch {
	case errors.Is(err, mcptoken.ErrNotFound):
		settingshttp.Toast(c, http.StatusNotFound, "そのトークンは見つかりません")
		return
	case err != nil:
		_ = c.Error(err)
		settingshttp.Toast(c, http.StatusInternalServerError, "トークンを失効できませんでした。mam のログを確認してください")
		return
	}
	d := SectionData(c.Request.Context(), h.Svc)
	i := slices.IndexFunc(d.Tokens, func(t view.TokenRow) bool { return t.ID == id })
	if i < 0 { // revoked, then gone from the list (cannot be read): the toast alone says it worked
		settingshttp.Render(c, http.StatusOK, atoms.SuccessToastOOB("トークンを失効しました。このトークンでの接続はできなくなりました"))
		return
	}
	settingshttp.Render(c, http.StatusOK, templ.Join(view.TokenRowItem(d.Tokens[i], false), atoms.SuccessToastOOB("トークンを失効しました。このトークンでの接続はできなくなりました")))
}
