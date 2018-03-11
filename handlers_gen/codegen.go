package main

// код писать тут

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"reflect"
	"strings"
	"text/template"
)

//-----------------------------------------------------------------------------
type (
	tpl1 struct { // ParamField
		StructTypeName string
		FieldName      string
		JsonName       string
		MustSign       string
		Value          int
		EnumStr        []string
		EnumInt        []int //TODO ???
		DefaultStr     string
		DefaultInt     int //TODO ???
	}

	funcData struct {
		agMRecvType   string //agMethodReceiverTypeName
		agFName       string //agFuncName
		agParamsType  string //agParamsTypeName
		agMResultType string //agMethodResultTypeName
		agData        ApigenFuncData
	}

	tpl2 struct { // serveHTTP
		agMRecvType  string //api struct
		agMRecvFuncs []funcData
	}

	ApigenFuncData struct {
		Url    string `json:"url"`
		Auth   bool   `json:"auth"`
		Method string `json:"method"`
	}

	tpls2 map[string]tpl2 //map[Recv]tpl2
)

//-----------------------------------------------------------------------------
const (
	apigenPrefix = "// apigen:api"
)

//-----------------------------------------------------------------------------
var (
	strFillTpl = template.Must(template.New("strFillTpl").Parse(`
	// paramsFillString_{{.FieldName}}
	params.{{.FieldName}} = r.FormValue("{{.JsonName}}")
`))
	intFillTpl = template.Must(template.New("intFillTpl").Parse(`
	// paramsFillInt_{{.FieldName}}
	if tmp, err := strconv.Atoi(r.FormValue("{{.JsonName}}")); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "{\"error\":\"{{.JsonName}} must be int\"}")
		return
	} else {
		params.{{.FieldName}} = tmp
	}
`))

	avStrRequiredTpl = template.Must(template.New("avStrRequiredTpl").Parse(`
	// paramsValidateStrRequired_{{.StructTypeName}}.{{.FieldName}}
	if params.{{.FieldName}} == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "{\"error\":\"{{.JsonName}} must me not empty\"}")
		return
	}
`))
	avStrMinLenTpl = template.Must(template.New("avStrMinLenTpl").Parse(`
	// paramsValidateStrMinLen_{{.StructTypeName}}.{{.FieldName}}
	if !len(params.{{.FieldName}}) >= {{.Value}} {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "{\"error\":\"{{.JsonName}} len must be >= {{.Value}}\"}")
		return
	}	
`))
	avStrEnumTpl = template.Must(template.New("avStrEnumTpl").Funcs(template.FuncMap{
		"join": strings.Join}).Parse(`
	// paramsValidateStrEnum_{{.StructTypeName}}.{{.FieldName}}
	switch params.{{.FieldName}} {
	case "{{join .EnumStr "\", \""}}": //do nothing
	case "":
		params.{{.FieldName}} = "{{.DefaultStr}}"
	default:
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "{\"error\":\"{{.JsonName}} must be one of [{{join .EnumStr ", "}}]\"}")
		return
	}
`))
	avIntMinMaxTpl = template.Must(template.New("avIntMinMaxTpl").Parse(`
	// paramsValidateIntMinMax_{{.StructTypeName}}.{{.FieldName}}
	if !params.{{.FieldName}} {{.MustSign}} {{.Value}} {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "{\"error\":\"{{.JsonName}} must be {{.MustSign}} {{.Value}}\"}")
		return
	}
`))

	serveHttpTpl = template.Must(template.New("serveHttpTpl").Parse(`
func (h {{.agMRecvType}}) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	{{range .agMRecvFuncs}}case "{{.agData.Url}}":
		wrapper{{.agFuncName}}(w, r)
	{{end}}
	default:
		w.WriteHeader(http.StatusNotFound) //404
		fmt.Fprintln(w, "{\"error\":\"unknown method\"}")
	}
`))
)

//-----------------------------------------------------------------------------
func main() {
	tmpStructs := make(tmpStructStorage) //TODO ot //for structs to process later

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, os.Args[1], nil, parser.ParseComments)
	if err != nil {
		log.Fatal(err)
	}

	out, _ := os.Create(os.Args[2])

	fmt.Fprintln(out, `package `+node.Name.Name)
	fmt.Fprintln(out) // empty line
	fmt.Fprintln(out, `import "encoding/json"`)
	fmt.Fprintln(out, `import "fmt"`)
	fmt.Fprintln(out, `import "net/http"`)
	fmt.Fprintln(out, `import "strconv"`)
	fmt.Fprintln(out) // empty line

ROOT_NODE_DECLS:
	for _, f := range node.Decls {
		switch d := f.(type) {
		//======================================================================
		/*
			case *ast.GenDecl:
				fmt.Println("== GenDecl:", d)

				//SPECS_LOOP:
				for _, spec := range d.Specs {
					currType, ok := spec.(*ast.TypeSpec)
					if !ok {
						fmt.Printf("SKIP %T is not ast.TypeSpec\n", spec)
						continue
					}

					currStruct, ok := currType.Type.(*ast.StructType)
					if !ok {
						fmt.Printf("SKIP %T is not ast.StructType\n", currStruct)
						continue
					}

					tmpStructName := currType.Name.Name
					fmt.Printf("process struct %s\n", tmpStructName)
					fmt.Printf("\tgenerating auxiliary data structures from struct definition\n")

					//TODO
					fmt.Println("TODO", tmpStructs)

				FIELDS_LOOP:
					for _, field := range currStruct.Fields.List {

						if field.Tag != nil {
							fmt.Println("==== struct field tag:", field.Tag.Value)
							tag := reflect.StructTag(field.Tag.Value[1 : len(field.Tag.Value)-1])
							tagStr := tag.Get("apivalidator")
							if tagStr == "" {
								continue FIELDS_LOOP
							}
							fmt.Println("==== tagStr:", tagStr)
						}

						fieldName := field.Names[0].Name
						fileType := field.Type.(*ast.Ident).Name

						fmt.Printf("\tgenerating code for field %s.%s\n", currType.Name.Name, fieldName)

						switch fileType {
						case "int":
							intFillTpl.Execute(out, tpl{fieldName})
						case "string":
							strFillTpl.Execute(out, tpl{fieldName})
						default:
							log.Fatalln("unsupported", fileType)
						}
					}

					fmt.Fprintln(out) // empty line

				}

		*/
		//======================================================================
		case *ast.FuncDecl:
			if d.Doc == nil {
				fmt.Printf("SKIP func %#v doesnt have comments\n", d.Name.Name)
				continue ROOT_NODE_DECLS
			}

			// Searching apigen tag of the func //struct method
			agFuncData := ApigenFuncData{}
			needCodegen := false
			for _, comment := range d.Doc.List {
				if strings.HasPrefix(comment.Text, apigenPrefix) { //TODO ??? Presuming only_one OR none apigenPrefix (is it OK?)
					fdJson := []byte(strings.TrimLeft(comment.Text, apigenPrefix))
					err := json.Unmarshal(fdJson, &agFuncData)
					if err != nil {
						fmt.Printf("ERROR func %#v has corrupted apigen json: %s", d.Name.Name, comment.Text)
						continue ROOT_NODE_DECLS
					}
					needCodegen = true
					break
				}
			}
			if !needCodegen {
				fmt.Printf("SKIP func %#v doesnt have apigen mark\n", d.Name.Name)
				continue ROOT_NODE_DECLS
			}

			// Method receiver type name =======================================
			r := d.Recv
			if r == nil {
				fmt.Printf("SKIP non method %#v\n", d.Name.Name)
				continue ROOT_NODE_DECLS
			}
			//if r.List == nil { continue ROOT_NODE_DECLS } //TODO ?
			//for _, cc := range agr.List { //TODO ?
			var agMRecvType string //++ agMethodReceiverTypeName
			star, ok := r.List[0].Type.(*ast.StarExpr)
			if ok {
				i, _ := star.X.(*ast.Ident) //TODO !ok ?
				agMRecvType = "*" + i.Name
			} else { // !StarExpr ~ method receiver by value
				i, _ := r.List[0].Type.(*ast.Ident) //TODO !ok ?
				agMRecvType = i.Name
			}
			fmt.Println("=== agMRecvType ===", agMRecvType)

			// Func (method) name ==============================================
			agFName := d.Name.Name //++ agFuncName
			fmt.Println("\n\n=== agFName ===", agFName)

			// Method Params ===================================================
			//if len(d.Type.Params.List) != 2 { continue ROOT_NODE_DECLS } //TODO ??? ###PRESUME### our method has 2 params
			s := d.Type.Params.List[1] //TODO ??? ###PRESUME### our Params struct is method's 2nd (and last) param
			//if len(s.Names) != 1 { continue ROOT_NODE_DECLS } //TODO ??? ###PRESUME### "in Params", NOT "in1, in2 Params"
			n := s.Names[0]
			f, _ := n.Obj.Decl.(*ast.Field) //TODO !ok ?
			t, _ := f.Type.(*ast.Ident)     //TODO !ok ?
			agParamsType := t.Name          //++ agParamsTypeName
			fmt.Println("=== agParamsType ===", agParamsType)

			// Method Results ==================================================
			var agMResultType string                                 //++ agMethodResultTypeName
			star2, ok := d.Type.Results.List[0].Type.(*ast.StarExpr) //TODO ??? ###PRESUME### we need 1st result
			if ok {
				i, _ := star2.X.(*ast.Ident) //TODO !ok ?
				agMResultType = "*" + i.Name
			} else { // !StarExpr ~ result by value
				i, _ := r.List[0].Type.(*ast.Ident) //TODO !ok ?
				agMResultType = i.Name
			}
			fmt.Println("=== agMResultType ===", agMResultType)

			//==================================================================
			//TODO Params -> go deeper
			//==================================================================

		default:
			fmt.Println("SKIP %T is not ast.GenDecl or ast.FuncDecl\n", d)
		}

		fmt.Println("\n\n") //DEBUG
	}
}
