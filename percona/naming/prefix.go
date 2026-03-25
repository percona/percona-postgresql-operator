package naming

import "strings"

const (
	PrefixPercona         = "percona.com/"
	PrefixPerconaInternal = "internal." + PrefixPercona
	PrefixPerconaPGV2     = "pgv2.percona.com/"
	PrefixCrunchy         = "postgres-operator.crunchydata.com/"
)

func ToCrunchyAnnotation(annotation string) string {
	return replacePrefix(annotation, PrefixPerconaPGV2, PrefixCrunchy)
}

func replacePrefix(s, oldPrefix, newPrefix string) string {
	s, found := strings.CutPrefix(s, oldPrefix)
	if found {
		return newPrefix + s
	}
	return s
}
