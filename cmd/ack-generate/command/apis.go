// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package command

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	awssdkmodel "github.com/aws/aws-sdk-go/private/model/api"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/ghodss/yaml"
	"github.com/iancoleman/strcase"
	"github.com/spf13/cobra"

	"github.com/aws/aws-service-operator-k8s/pkg/model"
	"github.com/aws/aws-service-operator-k8s/pkg/schema"
	template "github.com/aws/aws-service-operator-k8s/pkg/template/apis"
)

type contentType int

const (
	ctUnknown contentType = iota
	ctJSON
	ctYAML
)

var (
	optGenVersion     string
	optAPIsInputPath  string
	optAPIsOutputPath string
	apisVersionPath   string
)

// apiCmd is the command that generates service API types
var apisCmd = &cobra.Command{
	Use:   "apis <service>",
	Short: "Generate Kubernetes API type definitions for an AWS service API",
	RunE:  generateAPIs,
}

func init() {
	apisCmd.PersistentFlags().StringVar(
		&optGenVersion, "version", "v1alpha1", "the resource API Version to use when generating API infrastructure and type definitions",
	)
	apisCmd.PersistentFlags().StringVarP(
		&optAPIsInputPath, "input", "i", "", "path to OpenAPI3 Schema document (optional, defaults to stdin)",
	)
	apisCmd.PersistentFlags().StringVarP(
		&optAPIsOutputPath, "output", "o", "", "path to directory for service controller to create generated files. Defaults to "+optServicesDir+"/$service",
	)
	rootCmd.AddCommand(apisCmd)
}

// generateAPIs generates the Go files for each resource in the AWS service
// API.
func generateAPIs(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("please specify the service alias for the AWS service API to generate")
	}
	svcAlias := strings.ToLower(args[0])
	if optAPIsOutputPath == "" {
		optAPIsOutputPath = filepath.Join(optServicesDir)
	}
	if !optDryRun {
		apisVersionPath = filepath.Join(optAPIsOutputPath, svcAlias, "apis", optGenVersion)
		if _, err := ensureDir(apisVersionPath); err != nil {
			return err
		}
	}
	ensureSDKRepo(optCacheDir)
	sdkHelper := model.NewSDKHelper(sdkDir)
	sdkAPI, err := sdkHelper.API(svcAlias)
	if err != nil {
		return err
	}
	sh, err := getSchemaHelper(sdkAPI)
	if err != nil {
		return err
	}

	crds, err := sh.GetCRDs()
	if err != nil {
		return err
	}
	typeDefs, err := sh.GetTypeDefs()
	if err != nil {
		return err
	}
	enumDefs, err := sh.GetEnumDefs()
	if err != nil {
		return err
	}

	if err = writeDocGo(sh); err != nil {
		return err
	}

	if err = writeGroupVersionInfoGo(sh); err != nil {
		return err
	}

	if err = writeEnumsGo(enumDefs); err != nil {
		return err
	}

	if err = writeTypesGo(typeDefs); err != nil {
		return err
	}

	for _, crd := range crds {
		if err = writeCRDGo(crd); err != nil {
			return err
		}
	}
	return nil
}

func writeDocGo(sh *schema.Helper) error {
	var b bytes.Buffer
	apiGroup := sh.GetAPIGroup()
	vars := &template.DocTemplateVars{
		APIVersion: optGenVersion,
		APIGroup:   apiGroup,
	}
	tpl, err := template.NewDocTemplate(optTemplatesDir)
	if err != nil {
		return err
	}
	if err := tpl.Execute(&b, vars); err != nil {
		return err
	}
	if optDryRun {
		fmt.Println("============================= doc.go ======================================")
		fmt.Println(strings.TrimSpace(b.String()))
		return nil
	}
	path := filepath.Join(apisVersionPath, "doc.go")
	return ioutil.WriteFile(path, b.Bytes(), 0666)
}

func writeGroupVersionInfoGo(sh *schema.Helper) error {
	var b bytes.Buffer
	apiGroup := sh.GetAPIGroup()
	vars := &template.GroupVersionInfoTemplateVars{
		APIVersion: optGenVersion,
		APIGroup:   apiGroup,
	}
	tpl, err := template.NewGroupVersionInfoTemplate(optTemplatesDir)
	if err != nil {
		return err
	}
	if err := tpl.Execute(&b, vars); err != nil {
		return err
	}
	if optDryRun {
		fmt.Println("============================= groupversion_info.go ======================================")
		fmt.Println(strings.TrimSpace(b.String()))
		return nil
	}
	path := filepath.Join(apisVersionPath, "groupversion_info.go")
	return ioutil.WriteFile(path, b.Bytes(), 0666)
}

func writeEnumsGo(
	enumDefs []*model.EnumDef,
) error {
	if len(enumDefs) == 0 {
		return nil
	}
	vars := &template.EnumsTemplateVars{
		APIVersion: optGenVersion,
		EnumDefs:   enumDefs,
	}
	var b bytes.Buffer
	tpl, err := template.NewEnumsTemplate(optTemplatesDir)
	if err != nil {
		return err
	}
	if err := tpl.Execute(&b, vars); err != nil {
		return err
	}
	if optDryRun {
		fmt.Println("============================= enums.go ======================================")
		fmt.Println(strings.TrimSpace(b.String()))
		return nil
	}
	path := filepath.Join(apisVersionPath, "enums.go")
	return ioutil.WriteFile(path, b.Bytes(), 0666)
}

func writeTypesGo(
	typeDefs []*model.TypeDef,
) error {
	vars := &template.TypesTemplateVars{
		APIVersion: optGenVersion,
		TypeDefs:   typeDefs,
	}
	var b bytes.Buffer
	tpl, err := template.NewTypesTemplate(optTemplatesDir)
	if err != nil {
		return err
	}
	if err := tpl.Execute(&b, vars); err != nil {
		return err
	}
	if optDryRun {
		fmt.Println("============================= types.go ======================================")
		fmt.Println(strings.TrimSpace(b.String()))
		return nil
	}
	path := filepath.Join(apisVersionPath, "types.go")
	return ioutil.WriteFile(path, b.Bytes(), 0666)
}

func writeCRDGo(crd *model.CRD) error {
	vars := &template.CRDTemplateVars{
		APIVersion: optGenVersion,
		CRD:        crd,
	}
	var b bytes.Buffer
	tpl, err := template.NewCRDTemplate(optTemplatesDir)
	if err != nil {
		return err
	}
	if err := tpl.Execute(&b, vars); err != nil {
		return err
	}
	crdFileName := strcase.ToSnake(crd.Kind) + ".go"
	if optDryRun {
		fmt.Printf("============================= %s ======================================\n", crdFileName)
		fmt.Println(strings.TrimSpace(b.String()))
		return nil
	}
	path := filepath.Join(apisVersionPath, crdFileName)
	return ioutil.WriteFile(path, b.Bytes(), 0666)
}

// getAPI returns a schema.Helper object representing the API from
// either STDIN or an input file
func getSchemaHelper(sdkAPI *awssdkmodel.API) (*schema.Helper, error) {
	var b []byte
	var err error
	contentType := ctUnknown
	if optAPIsInputPath == "" {
		if b, err = ioutil.ReadAll(os.Stdin); err != nil {
			return nil, fmt.Errorf("expected OpenAPI3 descriptor document either via STDIN or path argument")
		}
	} else {
		fp := filepath.Clean(optAPIsInputPath)
		ext := filepath.Ext(fp)
		switch ext {
		case "json":
			contentType = ctJSON
		case "yaml", "yml":
			contentType = ctYAML
		}
		if b, err = ioutil.ReadFile(fp); err != nil {
			return nil, err
		}
	}

	var jsonb = b

	// First get our supplied document into JSON format
	if contentType == ctYAML || (contentType == ctUnknown && b[0] != '{' && b[0] != '[') {
		// It's probably YAML, so try decoding to YAML first and fall back to
		// JSON below
		if jsonb, err = yaml.YAMLToJSON(b); err != nil {
			jsonb = b
		}
	}

	a, err := openapi3.NewSwaggerLoader().LoadSwaggerFromData(jsonb)
	if err != nil {
		return nil, err
	}
	return schema.NewHelper(a, sdkAPI), nil
}
