package pending_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPending(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Pending Sweep Suite")
}
