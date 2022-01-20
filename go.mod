module github.com/percona/percona-postgresql-operator

go 1.15

require (
	github.com/fatih/color v1.9.0
	github.com/go-openapi/errors v0.19.8
	github.com/go-openapi/runtime v0.19.24
	github.com/go-openapi/strfmt v0.20.1
	github.com/go-openapi/swag v0.19.12
	github.com/go-openapi/validate v0.19.14
	github.com/gorilla/mux v1.7.4
	github.com/iancoleman/orderedmap v0.2.0
	github.com/mattn/go-colorable v0.1.6 // indirect
	github.com/nsqio/go-nsq v1.0.8
	github.com/pkg/errors v0.9.1
	github.com/robfig/cron/v3 v3.0.1
	github.com/sirupsen/logrus v1.7.0
	github.com/spf13/cobra v0.0.5
	github.com/spf13/pflag v1.0.5
	github.com/xdg/stringprep v1.0.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.13.0
	go.opentelemetry.io/otel v0.13.0
	go.opentelemetry.io/otel/exporters/stdout v0.13.0
	go.opentelemetry.io/otel/exporters/trace/jaeger v0.13.0
	golang.org/x/crypto v0.0.0-20210220033148-5ea612d1eb83
	golang.org/x/sync v0.0.0-20201020160332-67f06af15bc9
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.21.0
	k8s.io/apimachinery v0.21.0
	k8s.io/client-go v0.21.0
	sigs.k8s.io/controller-runtime v0.6.4
	sigs.k8s.io/yaml v1.2.0
)

exclude (
	go.mongodb.org/mongo-driver v1.0.3
	go.mongodb.org/mongo-driver v1.0.4
	go.mongodb.org/mongo-driver v1.1.0
	go.mongodb.org/mongo-driver v1.1.1
	go.mongodb.org/mongo-driver v1.1.2
	go.mongodb.org/mongo-driver v1.1.3
	go.mongodb.org/mongo-driver v1.1.4
	go.mongodb.org/mongo-driver v1.2.0
	go.mongodb.org/mongo-driver v1.2.1
	go.mongodb.org/mongo-driver v1.3.0
	go.mongodb.org/mongo-driver v1.3.1
	go.mongodb.org/mongo-driver v1.3.2
	go.mongodb.org/mongo-driver v1.3.3
	go.mongodb.org/mongo-driver v1.3.4
	go.mongodb.org/mongo-driver v1.3.5
	go.mongodb.org/mongo-driver v1.3.6
	go.mongodb.org/mongo-driver v1.3.7
	go.mongodb.org/mongo-driver v1.4.0
	go.mongodb.org/mongo-driver v1.4.0-beta1
	go.mongodb.org/mongo-driver v1.4.0-beta2
	go.mongodb.org/mongo-driver v1.4.0-rc0
	go.mongodb.org/mongo-driver v1.4.1
	go.mongodb.org/mongo-driver v1.4.2
	go.mongodb.org/mongo-driver v1.4.3
	go.mongodb.org/mongo-driver v1.4.4
	go.mongodb.org/mongo-driver v1.4.5
	go.mongodb.org/mongo-driver v1.4.6
	go.mongodb.org/mongo-driver v1.4.7
	go.mongodb.org/mongo-driver v1.5.0
	go.mongodb.org/mongo-driver v1.5.0-beta1
)
