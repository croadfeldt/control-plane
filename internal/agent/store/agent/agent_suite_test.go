package agent_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAgentStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Agent Store Suite")
}
