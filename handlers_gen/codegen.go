package main

// код писать тут

//TODO ? Местами код дублируется для удобства применения шаблона,
// например, тип структуры:
// map[StructType]struct{StructType...}

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	//	"reflect"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

//-----------------------------------------------------------------------------
type (
	funcData struct {
		agMRecvType   string //agMethodReceiverType
		agFName       string //agFuncName
		agParamsType  string //agParamsType
		agMResultType string //agMethodResultType
		agMetaData    ApigenFuncMetaData
	}

	ApigenFuncMetaData struct {
		Url    string `json:"url"`
		Auth   bool   `json:"auth"`
		Method string `json:"method"`
	}

	//TODO ??? RestrictionEnumInt, RestrictionDefaultInt

	AVRequired struct {
		Value bool
	}
	AVParamName struct {
		Value string
	}
	AVRestrictionMin struct {
		Value int
	}
	AVRestrictionMax struct {
		Value int
	}
	AVRestrictionEnum struct { // !struct, just slice(string) because it can be nil too???
		Value []string
	}
	AVRestrictionDefault struct {
		Value string
	}
	//	AVRestrictionLenStr struct {
	//		Value int
	//	}

	// Data from `apivalidator:...` meta tag
	// At this point it's no matter of which type param is
	ApiValidatorMeta struct {
		Required     *AVRequired
		ParamName    *AVParamName
		RestrMin     *AVRestrictionMin //~RestrMinLenStr
		RestrMax     *AVRestrictionMax
		RestrEnum    *AVRestrictionEnum
		RestrDefault *AVRestrictionDefault
		//RestrMinLenStr  AVRestrictionLenStr
	}

	// template	for param validation
	//TODO rename avParamTplStruct ?
	//TODO Restr<...> --> []Restriction; type Restriction interface{ Parse(), Generate() }
	tplStruct1 struct { // ParamField
		StructType string
		FieldName  string
		FieldType  string //TODO ??? enum
		ApiValidatorMeta
	}

	// templates for SmthParams
	tplStructs1 map[string][]tplStruct1 //map[paramsStructType][]paramsStructField

	// template for (wrapperSmth func || serveHTTP.switch_case)
	tplStruct2 struct { // serveHTTP
		agMRecvType  string //api struct
		agMRecvFuncs []funcData
	}

	// templates for (func (h Smth) wrapper... || func (h Smth) serveHTTP)
	tplStructs2 map[string]*tplStruct2 //map[Recv]tpl2

)

//-----------------------------------------------------------------------------
//TODO ??? empty values (for example, "default=,") - how to process
func PopApiValidatorValue(s0 string, re *regexp.Regexp) (s, ss string) { //TODO ? (.., error)
	// dirty hack:
	if !strings.HasPrefix(s0, ",") {
		s0 = "," + s0
	}
	if !strings.HasSuffix(s0, ",") {
		s0 = s0 + ","
	}

	i := re.FindStringSubmatchIndex(s0) //[] OR [x0, x1, y0, y1]
	if len(i) < 4 {                     //==0 ?
		return s0, "" // no value
	}
	s = s0[:i[0]+1] + s0[i[1]:]
	ss = s0[i[2]:i[3]]
	return
}

func (m *ApiValidatorMeta) ParseAVMetaTags(s string) {
	s, required := PopApiValidatorValue(s, reRequred)
	if required == "required" {
		m.Required = &AVRequired{true}
	}
	s, paramname := PopApiValidatorValue(s, reParamname)
	if paramname != "" {
		m.ParamName = &AVParamName{paramname}
	}
	s, minS := PopApiValidatorValue(s, reMin)
	if min, err := strconv.Atoi(minS); err == nil {
		m.RestrMin = &AVRestrictionMin{min}
	}
	s, maxS := PopApiValidatorValue(s, reMax)
	if max, err := strconv.Atoi(maxS); err == nil {
		m.RestrMax = &AVRestrictionMax{max}
	}
	s, enum := PopApiValidatorValue(s, reEnum)
	if enum != "" {
		m.RestrEnum = &AVRestrictionEnum{strings.Split(enum, "|")}
	}
	s, def := PopApiValidatorValue(s, reDefault)
	if def != "" {
		m.RestrDefault = &AVRestrictionDefault{def}
	}
}

//func (s *tplStruct1) set(ss tplStruct1) {
//	s = ss
//}

func (s *tplStruct2) appendFunc(fd funcData) {
	s.agMRecvFuncs = append(s.agMRecvFuncs, fd)
}

//-----------------------------------------------------------------------------
const (
	apigenPrefix       = "// apigen:api"
	apivalidatorPrefix = "`apivalidator:\"" //TODO \" ???
	apivalidatorSuffix = "\"`"
	inFName            = "/home/vit/programs/coursera-mail.ru-go/hw5_codegen/api.go" //TODO REMOVE DEBUG
)

//-----------------------------------------------------------------------------
var (
	//TODO ??? We will be using simpler regular expressions,
	// but then we have to add "," at the beginning and at the end of string to match against:
	// "`apivalidator:paramname=a,required,min=5`" -> "paramname=a,required,min=5" -> ",paramname=a,required,min=5,"
	reRequred   = regexp.MustCompile(",(required),")
	reParamname = regexp.MustCompile(",paramname=([^,]*)")
	reEnum      = regexp.MustCompile(",enum=([^,]*)") //TODO strings.Split(..,"|")
	reDefault   = regexp.MustCompile(",default=([^,]*)")
	reMin       = regexp.MustCompile(",min=([^,]*)")
	reMax       = regexp.MustCompile(",max=([^,]*)")

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
	// paramsValidateStrRequired_{{.StructType}}.{{.FieldName}}
	if params.{{.FieldName}} == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "{\"error\":\"{{.JsonName}} must me not empty\"}")
		return
	}
`))
	avStrMinLenTpl = template.Must(template.New("avStrMinLenTpl").Parse(`
	// paramsValidateStrMinLen_{{.StructType}}.{{.FieldName}}
	if !len(params.{{.FieldName}}) >= {{.Value}} {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "{\"error\":\"{{.JsonName}} len must be >= {{.Value}}\"}")
		return
	}	
`))
	avStrEnumTpl = template.Must(template.New("avStrEnumTpl").Funcs(template.FuncMap{
		"join": strings.Join}).Parse(`
	// paramsValidateStrEnum_{{.StructType}}.{{.FieldName}}
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
	// paramsValidateIntMinMax_{{.StructType}}.{{.FieldName}}
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

	theTplStructs1 tplStructs1
	theTplStructs2 tplStructs2
)

//==============================================================================
//==============================================================================
func main() {
	theTplStructs1 = make(tplStructs1)
	theTplStructs2 = make(tplStructs2)

	fset := token.NewFileSet()
	//node, err := parser.ParseFile(fset, os.Args[1], nil, parser.ParseComments) //TODO UNCOMMENT DEBUG
	node, err := parser.ParseFile(fset, inFName, nil, parser.ParseComments) //TODO REMOVE DEBUG
	if err != nil {
		log.Fatal(err)
	}

	//out, _ := os.Create(os.Args[2]) //TODO UNCOMMENT DEBUG
	out := os.Stdout //TODO REMOVE DEBUG

	fmt.Fprintln(out, `package `+node.Name.Name)
	fmt.Fprintln(out) // empty line
	fmt.Fprintln(out, `import "encoding/json"`)
	fmt.Fprintln(out, `import "fmt"`)
	fmt.Fprintln(out, `import "net/http"`)
	fmt.Fprintln(out, `import "strconv"`)
	fmt.Fprintln(out) // empty line

	//==========================================================================
	// Parsing input ===========================================================
ROOT_NODE_DECLS:
	for _, f := range node.Decls {
		switch d := f.(type) {
		case *ast.FuncDecl:
			if d.Doc == nil {
				fmt.Printf("SKIP func %#v doesnt have comments\n", d.Name.Name)
				continue ROOT_NODE_DECLS
			}

			// Searching apigen tag of the func //struct method
			agFuncData := ApigenFuncMetaData{}
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
			var agMRecvType string //++ agMethodReceiverType
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
			agParamsType := t.Name          //++ agParamsType
			fmt.Println("=== agParamsType ===", agParamsType)

			// Method Results ==================================================
			var agMResultType string                                 //++ agMethodResultType
			star2, ok := d.Type.Results.List[0].Type.(*ast.StarExpr) //TODO ??? ###PRESUME### we need 1st result
			if ok {
				i, _ := star2.X.(*ast.Ident) //TODO !ok ?
				agMResultType = "*" + i.Name
			} else { // !StarExpr ~ result by value
				i, _ := r.List[0].Type.(*ast.Ident) //TODO !ok ?
				agMResultType = i.Name
			}
			fmt.Println("=== agMResultType ===", agMResultType)

			// ADD method data to tmp_map[RecvStructType] ======================
			if _, ok := theTplStructs2[agMRecvType]; !ok { //init map element of type tpl2
				theTplStructs2[agMRecvType] = &tplStruct2{
					agMRecvType:  agMRecvType, //struct_field: local_var
					agMRecvFuncs: make([]funcData, 0, 1),
				}
			}
			theTplStructs2[agMRecvType].appendFunc(funcData{
				agMRecvType:   agMRecvType,
				agFName:       agFName,
				agParamsType:  agParamsType,
				agMResultType: agMResultType,
				agMetaData:    agFuncData,
			})
			fmt.Printf("+++ %#v\n", theTplStructs2[agMRecvType])

			//==================================================================
			//TODO Params -> go deeper
			//==================================================================
			//fmt.Printf("### %#v\n", t.Obj.Decl)
			tt, _ := t.Obj.Decl.(*ast.TypeSpec)
			//fmt.Printf("#### %#v\n", tt.Type)
			ttt, _ := tt.Type.(*ast.StructType)

		PARAM_FIELDS_LOOP:
			for _, f := range ttt.Fields.List {
				t, _ := f.Type.(*ast.Ident) //TODO !ok ?
				//fmt.Printf("=type= %v\n", t.Name)
				//fmt.Printf("=tags= %v\n", f.Tag.Value)
				if !strings.HasPrefix(f.Tag.Value, apivalidatorPrefix) {
					fmt.Println("\tSKIP param_field with no apivalidator prefix")
					continue PARAM_FIELDS_LOOP
				}
				agFType := t.Name //++ paramType -> tpl<paramType>
				agFMetaTags := strings.TrimPrefix(f.Tag.Value, apivalidatorPrefix)
				agFMetaTags = strings.TrimSuffix(agFMetaTags, apivalidatorSuffix) //++ paramValidatorMetaTags

				// Parsing apivalidator meta tags:
				agFMeta := ApiValidatorMeta{}
				agFMeta.ParseAVMetaTags(agFMetaTags)
				for _, n := range f.Names {
					fmt.Printf("=== %v\n", n.Name) //++ paramName
					if _, ok := theTplStructs1[agParamsType]; !ok {
						theTplStructs1[agParamsType] = make([]tplStruct1, 0) //init
					}
					if agFMeta.ParamName == nil { //JsonName
						agFMeta.ParamName = &AVParamName{strings.ToLower(agFName)}
					}
					theTplStructs1[agParamsType] = append(theTplStructs1[agParamsType], tplStruct1{
						StructType:       agParamsType,
						FieldName:        agFName,
						FieldType:        agFType,
						ApiValidatorMeta: agFMeta,
					})
				}
			}
			fmt.Printf("##### %#v\n", theTplStructs1[agParamsType])
			fmt.Println(theTplStructs1) //TODO REMOVE DEBUG
			//os.Exit(0)
			//##################################################################

		default:
			fmt.Printf("SKIP %T is not ast.GenDecl or ast.FuncDecl\n", d)
		}
		//fmt.Println("\n\n") //DEBUG
	}

	//==========================================================================
	// Generating output =======================================================
	//TODO ...
}
