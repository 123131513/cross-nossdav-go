module github.com/uccmisl/godash

go 1.24

require (
	github.com/cavaliercoder/grab v2.0.1-0.20200331080741-9f014744ee41+incompatible
	github.com/francoispqt/gojay v1.2.13
	github.com/golang/protobuf v1.5.2
	github.com/hashicorp/consul/api v1.4.0
	github.com/quic-go/masque-go v0.0.0
	github.com/quic-go/quic-go v0.59.0
	github.com/yosida95/uritemplate/v3 v3.0.2
	golang.org/x/net v0.43.0
	gonum.org/v1/gonum v0.7.0
	google.golang.org/grpc v1.19.0
)

require (
	github.com/armon/go-metrics v0.0.0-20180917152333-f0300d1749da // indirect
	github.com/dunglas/httpsfv v1.0.2 // indirect
	github.com/fatih/color v1.9.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.1 // indirect
	github.com/hashicorp/go-hclog v0.12.0 // indirect
	github.com/hashicorp/go-immutable-radix v1.0.0 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/golang-lru v0.5.0 // indirect
	github.com/hashicorp/serf v0.8.2 // indirect
	github.com/mattn/go-colorable v0.1.4 // indirect
	github.com/mattn/go-isatty v0.0.12 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.1.2 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	golang.org/x/crypto v0.41.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
	golang.org/x/text v0.28.0 // indirect
	google.golang.org/genproto v0.0.0-20190306203927-b5d61aea6440 // indirect
	google.golang.org/protobuf v1.26.0 // indirect
)

replace github.com/quic-go/quic-go => ../quic-go-v0.59

replace github.com/quic-go/masque-go => ../../../masque-go-original
