// Package main sunabar-probe は AWS 上の Fargate タスクなどから sunabar 実 API へ到達できるかを
// 単体で検証するためのワンショット CLI。 認証情報はソースコードに埋め込まず、 すべて環境変数から読む。
//
// 必須環境変数:
//
//	SUNABAR_BASE_URL          (例: https://api.sunabar.gmo-aozora.com)
//	SUNABAR_ACCESS_TOKEN      (sunabar サービスサイトで発行された個人アカウントのアクセストークン)
//
// 任意環境変数:
//
//	SUNABAR_ACCOUNT_ID                指定なら GetBalance / ListTransactions / 振込手数料事前照会も実行する 12 桁 accountId
//	SUNABAR_PROBE_DAYS                ListTransactions の取得日数 ( 既定: 30 )
//	SUNABAR_PROBE_WRITE               "transfer-fee" / "transfer" のいずれかなら書き込み系を試行
//	SUNABAR_PROBE_DEST_BANK           振込先銀行コード ( 既定 "0310" )
//	SUNABAR_PROBE_DEST_BRANCH         振込先支店コード ( 既定 "502" )
//	SUNABAR_PROBE_DEST_ACCT_TYPE      振込先預金種目 ( 既定 "1" )
//	SUNABAR_PROBE_DEST_ACCT_NUMBER    振込先口座番号 ( 既定 "1234567" )
//	SUNABAR_PROBE_DEST_NAME_KANA      振込先半角カナ受取人名 ( 既定 "ｻﾝﾌﾟﾙ" )
//	SUNABAR_PROBE_AMOUNT              振込額 ( 既定 1 )
//	SUNABAR_ACCESS_TOKEN_CORPORATE    法人 API 用トークン ( 設定時のみ /corporation/v1/accounts でも疎通テスト )
//
// 出力は構造化ログ ( slog JSON ) で stdout に書き、 成功時 exit 0、 失敗時 exit 1 で終了する。
// CloudWatch Logs から「AWS から sunabar に通信できた」ことが追跡可能。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"go-sunabar-payments/internal/platform/sunabar"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("sunabar-probe failed", "err", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	baseURL := os.Getenv("SUNABAR_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.sunabar.gmo-aozora.com"
	}
	token := os.Getenv("SUNABAR_ACCESS_TOKEN")
	if token == "" {
		return errors.New("SUNABAR_ACCESS_TOKEN is empty")
	}

	client, err := buildClient(baseURL, token)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger.Info("probe start", "baseURL", baseURL, "tokenSuffix", lastN(token, 4))

	accs, err := client.GetAccounts(ctx)
	if err != nil {
		return fmt.Errorf("GetAccounts: %w", err)
	}
	logger.Info("GetAccounts ok", "count", len(accs), "summary", summarizeAccounts(accs))

	accountID := resolveAccountID(accs)
	if accountID == "" {
		logger.Warn("no accountId; skipping balance/transactions")
		return nil
	}

	bal, err := client.GetBalance(ctx, accountID)
	if err != nil {
		return fmt.Errorf("GetBalance: %w", err)
	}
	logger.Info("GetBalance ok",
		"accountId", bal.AccountID,
		"currency", bal.CurrencyCode,
		"balance", bal.Balance,
		"baseDate", bal.BaseDate,
		"baseTime", bal.BaseTime,
	)

	tx, err := fetchTransactions(ctx, client, accountID)
	if err != nil {
		return err
	}
	logger.Info("ListTransactions ok",
		"accountId", tx.AccountID,
		"count", tx.Count,
		"hasNext", tx.HasNext,
		"baseDate", tx.BaseDate,
	)

	out := map[string]any{
		"accounts":     accs,
		"balance":      bal,
		"transactions": tx,
	}
	runWriteProbe(ctx, client, logger, accountID, out)

	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	logger.Info("probe done")
	return nil
}

// buildClient 環境変数から sunabar HTTPClient を生成する。
func buildClient(baseURL, token string) (sunabar.Client, error) {
	auth, err := sunabar.NewStaticTokenSource(token)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	cfg := sunabar.HTTPClientConfig{BaseURL: baseURL, Auth: auth}
	if corp := os.Getenv("SUNABAR_ACCESS_TOKEN_CORPORATE"); corp != "" {
		corpAuth, err := sunabar.NewStaticTokenSource(corp)
		if err != nil {
			return nil, fmt.Errorf("corp auth: %w", err)
		}
		cfg.CorporateAuth = corpAuth
	}
	client, err := sunabar.NewHTTPClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("client: %w", err)
	}
	return client, nil
}

func resolveAccountID(accs []sunabar.Account) string {
	if id := os.Getenv("SUNABAR_ACCOUNT_ID"); id != "" {
		return id
	}
	if len(accs) > 0 {
		return accs[0].AccountID
	}
	return ""
}

func fetchTransactions(ctx context.Context, client sunabar.Client, accountID string) (*sunabar.TransactionList, error) {
	days := 30
	if d := os.Getenv("SUNABAR_PROBE_DAYS"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 {
			days = n
		}
	}
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -days)
	tx, err := client.ListTransactions(ctx, accountID, sunabar.ListTransactionsParams{From: from, To: to})
	if err != nil {
		return nil, fmt.Errorf("ListTransactions: %w", err)
	}
	return tx, nil
}

// runWriteProbe SUNABAR_PROBE_WRITE が設定されているときのみ書き込み系の疎通テストを行う。
func runWriteProbe(ctx context.Context, client sunabar.Client, logger *slog.Logger, accountID string, out map[string]any) {
	mode := os.Getenv("SUNABAR_PROBE_WRITE")
	switch mode {
	case "":
		return
	case "transfer":
		req := buildTransferReq(accountID)
		res, err := client.RequestTransfer(ctx, req)
		if err != nil {
			logger.Warn("RequestTransfer error ( 通信そのものは成功した可能性あり )", "err", err.Error())
			out["transfer_error"] = err.Error()
			return
		}
		logger.Info("RequestTransfer ok", "applyNo", res.ApplyNo, "status", res.Status)
		out["transfer"] = res
	case "transfer-fee":
		// transfer-fee 単体エンドポイントは Client インターフェース未実装のためスキップ。
		logger.Info("transfer-fee skipped", "hint", "RequestTransfer を amount=1 で投げて 400 残高不足を観測することで疎通検証可能")
	default:
		logger.Warn("unknown SUNABAR_PROBE_WRITE value; skipping write probes", "value", mode)
	}
}

// buildTransferReq 環境変数から振込依頼リクエストを組み立てる。 受取人名は半角カナ必須。
func buildTransferReq(srcAccountID string) sunabar.TransferRequest {
	bank := envOr("SUNABAR_PROBE_DEST_BANK", "0310")
	branch := envOr("SUNABAR_PROBE_DEST_BRANCH", "502")
	acctType := envOr("SUNABAR_PROBE_DEST_ACCT_TYPE", "1")
	acctNum := envOr("SUNABAR_PROBE_DEST_ACCT_NUMBER", "1234567")
	name := envOr("SUNABAR_PROBE_DEST_NAME_KANA", "ｻﾝﾌﾟﾙ")
	amount := int64(1)
	if a := os.Getenv("SUNABAR_PROBE_AMOUNT"); a != "" {
		if n, err := strconv.ParseInt(a, 10, 64); err == nil && n > 0 {
			amount = n
		}
	}
	return sunabar.TransferRequest{
		IdempotencyKey:  "probe-" + time.Now().UTC().Format("20060102T150405"),
		SourceAccountID: srcAccountID,
		Amount:          amount,
		DestBankCode:    bank,
		DestBranchCode:  branch,
		DestAccountType: acctType,
		DestAccountNum:  acctNum,
		DestAccountName: name,
		// TransferDate は空にして HTTPClient 側で today を JST で埋める
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func summarizeAccounts(accs []sunabar.Account) []map[string]any {
	out := make([]map[string]any, 0, len(accs))
	for _, a := range accs {
		out = append(out, map[string]any{
			"accountId":   a.AccountID,
			"branchCode":  a.BranchCode,
			"primaryFlag": a.PrimaryAccountFlag,
		})
	}
	return out
}

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
