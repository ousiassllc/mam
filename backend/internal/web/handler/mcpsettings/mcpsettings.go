// Package mcpsettings holds the handlers of the "MCP サーバー" section of the global settings screen
// (spec/api/endpoints.md). It is a package of its own because handler is at its size limit; like handler,
// it depends on the service layer and the web layer only.
package mcpsettings

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/mcpsettings"
	"github.com/ousiassllc/mam/backend/internal/service/mcptoken"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/mcppanel"
)

// SectionData is the section as the settings screen shows it (the page handler of the settings screen
// calls it). When the tokens cannot be read it still shows the listener, with the reason as the error.
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
		d.Tokens = append(d.Tokens, view.TokenRow{
			ID: t.ID, Name: t.Name, Prefix: t.Prefix, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt, Revoked: t.Revoked(),
		})
	}
	return d
}

// Bind moves the MCP listener to the address of the form (PUT /settings/mcp/bind, field bind_address) and
// answers with the section: 200 with the notice, 422 with the reason when the address is not allowed, 500
// when the listener could not be opened or the setting saved (the old listener keeps serving either way).
func Bind(svc *mcpsettings.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		addr := strings.TrimSpace(c.PostForm("bind_address"))
		err := svc.Rebind(addr)
		d := SectionData(c.Request.Context(), svc)
		status := http.StatusOK
		var rejected *mcpsettings.RejectedError
		switch {
		case err == nil:
			d.Notice = "待ち受け先を " + addr + " に切り替えました。接続し直してください"
		case errors.As(err, &rejected):
			status, d.Error, d.TypedAddress = http.StatusUnprocessableEntity, rejected.Message, addr
		default:
			_ = c.Error(err)
			status, d.Error, d.TypedAddress = http.StatusInternalServerError, "待ち受け先を切り替えられませんでした（元の待ち受け先のままです）。ポートの空きと mam のログを確認してください", addr
		}
		render(c, status, view.Panel(d))
	}
}

// Issue issues a token named by the form (POST /settings/mcp/tokens, field name) and answers with the
// section holding the value, which is shown this once. The answer is not cacheable: the value must not
// stay in a cache or the history.
func Issue(svc *mcpsettings.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.PostForm("name")
		issued, err := svc.Tokens.Issue(c.Request.Context(), name)
		d := SectionData(c.Request.Context(), svc)
		status := http.StatusCreated
		var invalid *mcptoken.InvalidError
		switch {
		case err == nil:
			d.Issued = &view.NewToken{Name: issued.Token.Name, Value: issued.Value}
		case errors.As(err, &invalid):
			status, d.Error, d.TypedName = http.StatusUnprocessableEntity, invalid.Message, name
		default:
			_ = c.Error(err)
			status, d.Error, d.TypedName = http.StatusInternalServerError, "トークンを発行できませんでした。mam のログを確認してください", name
		}
		c.Header("Cache-Control", "no-store")
		render(c, status, view.Panel(d))
	}
}

// Revoke ends the token (DELETE /settings/mcp/tokens/:id) and answers with the section. A token that does
// not exist is 404 with the section and a reason.
func Revoke(svc *mcpsettings.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := svc.Tokens.Revoke(c.Request.Context(), c.Param("id"))
		d := SectionData(c.Request.Context(), svc)
		status := http.StatusOK
		switch {
		case err == nil:
			d.Notice = "トークンを失効しました。このトークンでの接続はできなくなりました"
		case errors.Is(err, mcptoken.ErrNotFound):
			status, d.Error = http.StatusNotFound, "そのトークンは見つかりません"
		default:
			_ = c.Error(err)
			status, d.Error = http.StatusInternalServerError, "トークンを失効できませんでした。mam のログを確認してください"
		}
		render(c, status, view.Panel(d))
	}
}

func render(c *gin.Context, status int, component templ.Component) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := component.Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
		if !c.Writer.Written() {
			c.AbortWithStatus(http.StatusInternalServerError)
		}
	}
}
