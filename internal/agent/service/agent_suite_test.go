package service_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAgentService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Agent Service Suite")
}
