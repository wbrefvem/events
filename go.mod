module github.com/trysterodev/events

go 1.13

require (
	github.com/drone/go-scm v1.7.0
	github.com/go-logr/logr v0.1.0
	github.com/onsi/ginkgo v1.12.0
	github.com/onsi/gomega v1.9.0
	github.com/tektoncd/pipeline v0.15.2
	k8s.io/api v0.17.6
	k8s.io/apimachinery v0.17.6
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	sigs.k8s.io/controller-runtime v0.5.0
)

replace k8s.io/client-go => k8s.io/client-go v0.17.6