package tt

type grammarRule struct {
	lhs string
	len int
}

type grammarState struct {
	actions map[string]int
	def     int
	hasDef  bool
	gotos   map[string]int
}
