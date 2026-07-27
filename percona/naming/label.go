package naming

const (
	LabelOperatorVersion = PrefixPerconaPGV2 + "version"

	// LabelLogicalReplica marks every object that belongs to a logical replica
	// with the replica's name. K8SPG-784
	LabelLogicalReplica = PrefixPerconaPGV2 + "logical-replica"
)
