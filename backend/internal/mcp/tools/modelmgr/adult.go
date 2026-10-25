package modelmgr

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

func registerAdult(r *mcpkit.Registrar, h handlers) {
	mcpkit.Read(r, "get_adult_content_status",
		"成人向けモデルの表示（年齢確認のゲート）の状態と、いまの注意書き（notice）とその版（notice_version）。enabled が false の間、成人向け（content_rating=adult）のモデルは list_model_catalog に出ず、導入・試し生成もできない / The age gate for adult models: whether it is open, and the notice (with its version) the user has to agree to.",
		h.adultStatus)
	mcpkit.Write(r, "set_adult_content",
		"成人向けモデルの表示を切り替える。オンにするには、先に get_adult_content_status の notice をユーザーに見せ、ユーザー自身が「18 歳以上（またはお住まいの地域で成年）であること」と注意書きへの同意を答えた後にだけ、confirm_adult=true と notice_version（get_adult_content_status の値）を付けて呼ぶ。エージェントが自分で確認したことにして呼んではならない（accept_model_license と同じ扱い）。一度確認すれば、オフにしてからもう一度オンにするときは confirm_adult を省ける。確認の日時と注意書きの版は mam が記録する / Turn the adult models on or off. Call it with confirm_adult=true only after the user themselves confirmed they are of age and agreed to the notice; never confirm on the user's behalf.",
		h.setAdult, mcpkit.Idempotent())
}

func (h handlers) adultStatus(_ context.Context, _ Input) (modelmanager.AdultContentStatus, error) {
	m, err := h.mm()
	if err != nil {
		return modelmanager.AdultContentStatus{}, err
	}
	return m.AdultContent(), nil
}

// AdultInput is the setting, with the user's confirmation when turning it on.
type AdultInput struct {
	Enabled       bool   `json:"enabled" jsonschema:"true: 成人向けモデルを表示する / false: 隠す（確認の記録は残る）"`
	ConfirmAdult  bool   `json:"confirm_adult,omitempty" jsonschema:"ユーザーが、18 歳以上（またはお住まいの地域で成年）であることと注意書きへの同意を、自分で答えたとき true。オンにする初回に必要。エージェントが代わりに true にしてはならない"`
	NoticeVersion string `json:"notice_version,omitempty" jsonschema:"ユーザーに見せた注意書きの版（get_adult_content_status の notice_version）。confirm_adult と一緒に渡す"`
}

func (h handlers) setAdult(_ context.Context, in AdultInput) (modelmanager.AdultContentStatus, error) {
	m, err := h.mm()
	if err != nil {
		return modelmanager.AdultContentStatus{}, err
	}
	var confirmation *modelmanager.AdultConfirmation
	if in.ConfirmAdult || in.NoticeVersion != "" {
		confirmation = &modelmanager.AdultConfirmation{Adult: in.ConfirmAdult, NoticeVersion: in.NoticeVersion}
	}
	if err := m.SetAdultContent(in.Enabled, confirmation); err != nil {
		return modelmanager.AdultContentStatus{}, MapError(err)
	}
	return m.AdultContent(), nil
}
