package healthcheck_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHealthCheck(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Agent Health Monitor Suite")
}
