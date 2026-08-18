package naming

const (
	LabelOperatorVersion = PrefixPerconaPGV2 + "version"

	// LabelLogicalReplica marks every object that belongs to a logical replica
	// with the replica's name.
	LabelLogicalReplica = PrefixPerconaPGV2 + "logical-replica"
)
