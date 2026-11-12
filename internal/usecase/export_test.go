package usecase

// テストからだけ見える出入口。本番のコードからは参照しない。
var (
	BuildUserMessage         = buildUserMessage
	ConnectionsSection       = connectionsSection
	ConnectionsSectionMaxLen = connectionsSectionMaxLen
	ParseInterpretation      = parseInterpretation
)
