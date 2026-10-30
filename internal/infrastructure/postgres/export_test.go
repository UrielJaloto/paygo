package postgres

var (
	RunInTransaction  = runInTransaction
	IsUniqueViolation = isUniqueViolation
	NotFound          = notFound
)

func NewQueryTracer(spans QuerySpans) queryTracer {
	return queryTracer{spans: spans}
}
