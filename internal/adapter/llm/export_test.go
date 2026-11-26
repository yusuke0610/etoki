package llm

import "net/http"

// テストからだけ見える出入口。本番のコードからは参照しない。

// HTTPClientOf は c が Messages API を叩くのに使う HTTP クライアントを返す。
func HTTPClientOf(c *Client) *http.Client { return c.http }
