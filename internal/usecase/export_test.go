package usecase

import "time"

// テストからだけ見える出入口。本番のコードからは参照しない。
var (
	BuildUserMessage         = buildUserMessage
	ConnectionsSection       = connectionsSection
	ConnectionsSectionMaxLen = connectionsSectionMaxLen
	ParseInterpretation      = parseInterpretation
)

// InterpretationJobTimeout は解釈のジョブが走り続けてよい上限を返す。
//
// 値で見る。ふつうは 1 回の呼び出しの上限が先に切るので、ジョブの上限が
// 長すぎても、呼び出しの結果には現れない。
func InterpretationJobTimeout(j *InterpretationJobs) time.Duration { return j.table.timeout }

// DiagramJobTimeout は図のドラフト生成のジョブが走り続けてよい上限を返す。
func DiagramJobTimeout(j *DiagramJobs) time.Duration { return j.table.timeout }
