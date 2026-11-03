package credential

import "errors"

// StoreError is a failure of the OS secure storage to keep an API key: it refused the write, took it and could not be read back, or
// took it and does not return it. Message is for the user and carries no key; Err is the cause for the log.
type StoreError struct {
	Message string
	Err     error
}

func (e *StoreError) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

// Unwrap returns the cause.
func (e *StoreError) Unwrap() error { return e.Err }

// notStoredMessage is what the user reads when the store took a key and does not return it. The keys are kept in the OS secure
// storage of the OS user that runs mam, under one service name for the whole app: a data directory (--data-dir) has no keys of
// its own, and another OS user (or a mam started as another user) sees a different store.
const notStoredMessage = "API キーを書き込みましたが、OS のセキュアストレージ（Windows の資格情報マネージャー・macOS のキーチェーン・Secret Service）から読み戻せませんでした。保存されていません。" +
	"キーは mam を動かしている OS ユーザーのストレージに入ります（データディレクトリごとには分かれません）。別の OS ユーザーで動かしていないか、ストレージが使えるかを確認してください"

// SetChecked is store.Set and then a read of the key back: a store that took the write and does not return the key is a *StoreError,
// never a silent success that shows as "not configured" later (#1006). ErrTooLarge is returned as it is (the caller words it).
func SetChecked(store Store, provider, apiKey string) error {
	err := store.Set(provider, apiKey)
	if errors.Is(err, ErrTooLarge) {
		return err
	}
	if err != nil {
		return &StoreError{Message: "OS のセキュアストレージに API キーを保存できませんでした", Err: err}
	}
	stored, err := store.Get(provider)
	switch {
	case err != nil && !errors.Is(err, ErrNotFound):
		return &StoreError{Message: "API キーを保存しましたが、OS のセキュアストレージから確かめられませんでした", Err: err}
	case err != nil || stored != apiKey:
		return &StoreError{Message: notStoredMessage}
	}
	return nil
}
