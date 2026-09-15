package cel_test

import (
	"github.com/dcm-project/control-plane/internal/cel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ParseReference", func() {
	It("parses a valid CEL reference", func() {
		ref, isReference, err := cel.ParseReference("${db.connection_string}")
		Expect(err).NotTo(HaveOccurred())
		Expect(isReference).To(BeTrue())
		Expect(ref.ResourceName).To(Equal("db"))
		Expect(ref.OutputField).To(Equal("connection_string"))
	})

	It("parses dotted output paths", func() {
		ref, isReference, err := cel.ParseReference("${db.config.host}")
		Expect(err).NotTo(HaveOccurred())
		Expect(isReference).To(BeTrue())
		Expect(ref.ResourceName).To(Equal("db"))
		Expect(ref.OutputField).To(Equal("config.host"))
	})

	It("parses indexed output paths", func() {
		ref, isReference, err := cel.ParseReference("${db.disks[0].name}")
		Expect(err).NotTo(HaveOccurred())
		Expect(isReference).To(BeTrue())
		Expect(ref.ResourceName).To(Equal("db"))
		Expect(ref.OutputField).To(Equal("disks[0].name"))
	})

	It("returns isReference=false for plain strings", func() {
		_, isReference, err := cel.ParseReference("postgres://localhost/db")
		Expect(err).NotTo(HaveOccurred())
		Expect(isReference).To(BeFalse())
	})

	It("rejects embedded CEL syntax", func() {
		_, isReference, err := cel.ParseReference("url-${db.connection_string}")
		Expect(err).To(HaveOccurred())
		Expect(isReference).To(BeTrue())
	})

	It("collects unique references from nested spec values", func() {
		spec := map[string]any{
			"database_url": "${db.connection_string}",
			"kubeconfig":   "${cluster.kubeconfig}",
			"nested": map[string]any{
				"url": "${db.connection_string}",
			},
			"endpoints": []any{"${svc.endpoint[0].address}"},
		}

		refs, err := cel.CollectReferences(spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(refs).To(ContainElement(cel.Reference{ResourceName: "db", OutputField: "connection_string"}))
		Expect(refs).To(ContainElement(cel.Reference{ResourceName: "cluster", OutputField: "kubeconfig"}))
		Expect(refs).To(ContainElement(cel.Reference{ResourceName: "svc", OutputField: "endpoint[0].address"}))
	})
})
