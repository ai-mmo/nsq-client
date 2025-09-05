module github.com/ai-mmo/nsq-client

go 1.24

replace mlog => github.com/ai-mmo/mlog v0.0.1

require (
	github.com/nsqio/go-nsq v1.1.0
	go.uber.org/zap v1.27.0
	gopkg.in/yaml.v3 v3.0.1
	mlog v0.0.0-00010101000000-000000000000
)

require (
	github.com/golang/snappy v1.0.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
)
