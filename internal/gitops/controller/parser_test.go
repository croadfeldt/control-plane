package controller_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/control-plane/internal/gitops/controller"
)

var _ = Describe("ParseCatalogItemInstances", func() {
	var tmpDir string

	BeforeEach(func() {
		tmpDir = GinkgoT().TempDir()
	})

	writeFile := func(name, content string) {
		err := os.MkdirAll(filepath.Join(tmpDir, "apps"), 0o755)
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(filepath.Join(tmpDir, "apps", name), []byte(content), 0o644)
		Expect(err).NotTo(HaveOccurred())
	}

	It("parses a valid CatalogItemInstance YAML", func() {
		writeFile("webserver.yaml", `
apiVersion: v1alpha1
kind: CatalogItemInstance
metadata:
  name: my-webserver
spec:
  display_name: My Webserver
  catalog_item_id: small-vm
  user_values:
    - resource: app
      path: vcpu.count
      value: 4
`)
		result := controller.ParseCatalogItemInstances(tmpDir, "apps")
		Expect(result.Errors).To(BeEmpty())
		Expect(result.Instances).To(HaveLen(1))
		Expect(result.Instances[0].Name).To(Equal("my-webserver"))
		Expect(result.Instances[0].DisplayName).To(Equal("My Webserver"))
		Expect(result.Instances[0].CatalogItemID).To(Equal("small-vm"))
		Expect(result.Instances[0].UserValues).To(HaveLen(1))
		Expect(result.Instances[0].UserValues[0].Path).To(Equal("vcpu.count"))
		Expect(result.Instances[0].SourceFile).To(Equal("apps/webserver.yaml"))
	})

	It("parses multiple YAML files in alphabetical order", func() {
		writeFile("beta.yaml", `
apiVersion: v1alpha1
kind: CatalogItemInstance
metadata:
  name: beta-app
spec:
  catalog_item_id: small-vm
  user_values: []
`)
		writeFile("alpha.yaml", `
apiVersion: v1alpha1
kind: CatalogItemInstance
metadata:
  name: alpha-app
spec:
  catalog_item_id: small-vm
  user_values: []
`)
		result := controller.ParseCatalogItemInstances(tmpDir, "apps")
		Expect(result.Errors).To(BeEmpty())
		Expect(result.Instances).To(HaveLen(2))
		Expect(result.Instances[0].Name).To(Equal("alpha-app"))
		Expect(result.Instances[1].Name).To(Equal("beta-app"))
	})

	It("last file alphabetically wins for duplicate names", func() {
		writeFile("alpha.yaml", `
apiVersion: v1alpha1
kind: CatalogItemInstance
metadata:
  name: same-name
spec:
  catalog_item_id: old-item
  user_values: []
`)
		writeFile("zebra.yaml", `
apiVersion: v1alpha1
kind: CatalogItemInstance
metadata:
  name: same-name
spec:
  catalog_item_id: new-item
  user_values: []
`)
		result := controller.ParseCatalogItemInstances(tmpDir, "apps")
		Expect(result.Errors).To(BeEmpty())
		Expect(result.Instances).To(HaveLen(1))
		Expect(result.Instances[0].CatalogItemID).To(Equal("new-item"))
	})

	It("skips invalid YAML and reports errors", func() {
		writeFile("bad.yaml", `this is not valid yaml: [`)
		writeFile("good.yaml", `
apiVersion: v1alpha1
kind: CatalogItemInstance
metadata:
  name: good-app
spec:
  catalog_item_id: small-vm
  user_values: []
`)
		result := controller.ParseCatalogItemInstances(tmpDir, "apps")
		Expect(result.Errors).To(HaveLen(1))
		Expect(result.Errors[0].File).To(Equal("bad.yaml"))
		Expect(result.Instances).To(HaveLen(1))
		Expect(result.Instances[0].Name).To(Equal("good-app"))
	})

	It("skips files with wrong kind", func() {
		writeFile("policy.yaml", `
apiVersion: v1alpha1
kind: Policy
metadata:
  name: my-policy
spec:
  catalog_item_id: something
  user_values: []
`)
		result := controller.ParseCatalogItemInstances(tmpDir, "apps")
		Expect(result.Errors).To(HaveLen(1))
		Expect(result.Errors[0].File).To(Equal("policy.yaml"))
		Expect(result.Instances).To(BeEmpty())
	})

	It("skips files missing metadata.name", func() {
		writeFile("noname.yaml", `
apiVersion: v1alpha1
kind: CatalogItemInstance
metadata:
  name: ""
spec:
  catalog_item_id: small-vm
  user_values: []
`)
		result := controller.ParseCatalogItemInstances(tmpDir, "apps")
		Expect(result.Errors).To(HaveLen(1))
		Expect(result.Instances).To(BeEmpty())
	})

	It("skips files missing spec.catalog_item_id", func() {
		writeFile("noid.yaml", `
apiVersion: v1alpha1
kind: CatalogItemInstance
metadata:
  name: my-app
spec:
  user_values: []
`)
		result := controller.ParseCatalogItemInstances(tmpDir, "apps")
		Expect(result.Errors).To(HaveLen(1))
		Expect(result.Instances).To(BeEmpty())
	})

	It("ignores non-YAML files", func() {
		writeFile("readme.txt", "This is not YAML")
		writeFile("app.yaml", `
apiVersion: v1alpha1
kind: CatalogItemInstance
metadata:
  name: my-app
spec:
  catalog_item_id: small-vm
  user_values: []
`)
		result := controller.ParseCatalogItemInstances(tmpDir, "apps")
		Expect(result.Errors).To(BeEmpty())
		Expect(result.Instances).To(HaveLen(1))
	})

	It("ignores subdirectories", func() {
		// Create a subdirectory with a YAML file
		subdir := filepath.Join(tmpDir, "apps", "subdir")
		err := os.MkdirAll(subdir, 0o755)
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(filepath.Join(subdir, "nested.yaml"), []byte(`
apiVersion: v1alpha1
kind: CatalogItemInstance
metadata:
  name: nested-app
spec:
  catalog_item_id: small-vm
  user_values: []
`), 0o644)
		Expect(err).NotTo(HaveOccurred())

		writeFile("root.yaml", `
apiVersion: v1alpha1
kind: CatalogItemInstance
metadata:
  name: root-app
spec:
  catalog_item_id: small-vm
  user_values: []
`)
		result := controller.ParseCatalogItemInstances(tmpDir, "apps")
		Expect(result.Errors).To(BeEmpty())
		Expect(result.Instances).To(HaveLen(1))
		Expect(result.Instances[0].Name).To(Equal("root-app"))
	})

	It("supports .yml extension", func() {
		writeFile("app.yml", `
apiVersion: v1alpha1
kind: CatalogItemInstance
metadata:
  name: yml-app
spec:
  catalog_item_id: small-vm
  user_values: []
`)
		result := controller.ParseCatalogItemInstances(tmpDir, "apps")
		Expect(result.Errors).To(BeEmpty())
		Expect(result.Instances).To(HaveLen(1))
		Expect(result.Instances[0].Name).To(Equal("yml-app"))
	})

	It("uses metadata.name as display_name when display_name is empty", func() {
		writeFile("app.yaml", `
apiVersion: v1alpha1
kind: CatalogItemInstance
metadata:
  name: my-app
spec:
  catalog_item_id: small-vm
  user_values: []
`)
		result := controller.ParseCatalogItemInstances(tmpDir, "apps")
		Expect(result.Errors).To(BeEmpty())
		Expect(result.Instances[0].DisplayName).To(Equal("my-app"))
	})

	It("returns error when path does not exist", func() {
		result := controller.ParseCatalogItemInstances(tmpDir, "nonexistent")
		Expect(result.Errors).To(HaveLen(1))
		Expect(result.Instances).To(BeEmpty())
	})
})
