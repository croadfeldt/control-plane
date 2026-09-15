package cel_test

import (
	"github.com/dcm-project/control-plane/internal/cel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BindReferences", func() {
	It("replaces CEL references with dependency output values", func() {
		spec := map[string]any{
			"database_url": "${db.connection_string}",
			"nested": map[string]any{
				"url": "${db.connection_string}",
			},
		}
		outputs := map[string]map[string]any{
			"db": {"connection_string": "postgres://db:5432/orders"},
		}

		bound, err := cel.BindReferences(spec, outputs)
		Expect(err).NotTo(HaveOccurred())
		Expect(bound["database_url"]).To(Equal("postgres://db:5432/orders"))
		Expect(bound["nested"].(map[string]any)["url"]).To(Equal("postgres://db:5432/orders"))
		Expect(spec["database_url"]).To(Equal("${db.connection_string}"))
	})

	It("returns an error when the dependency output is missing", func() {
		spec := map[string]any{"database_url": "${db.connection_string}"}
		_, err := cel.BindReferences(spec, map[string]map[string]any{"db": {"kind": "db"}})
		Expect(err).To(HaveOccurred())
	})

	It("returns an error when the dependency output type is not string", func() {
		spec := map[string]any{"database_url": "${db.connection_string}"}
		_, err := cel.BindReferences(spec, map[string]map[string]any{"db": {"connection_string": 5432}})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must be a string"))
	})
})
