package v1alpha1_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAgentHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Agent Handler Suite")
}
