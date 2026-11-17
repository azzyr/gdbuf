package codegen

import (
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/huandu/xstrings"
	"google.golang.org/protobuf/types/descriptorpb"
)

//go:embed templates/*
var templatesFS embed.FS

func toPascalCase(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

type CodeGenerator struct {
	logger                   *slog.Logger
	destinationDirectoryPath string
	templates                *template.Template
	extensionName            string
}

type templateData struct {
	GDExtensionName string
	ProtoData       protoData
	GlobalEnums     []protoEnum
}

type protoData struct {
	Files []protoFile
}

type protoFile struct {
	ProtoPath    string
	PathNoExt    string
	BaseName     string
	Dir          string
	Namespace    string
	PackageName  string
	Messages     []protoMessage
	Dependencies []string
	ForwardDecls []ForwardDecl
}

type ForwardDecl struct {
	Namespace string
	ClassName string
}

type protoMessage struct {
	ClassName   string
	MessageName string
	StructName  string
	Description string
	Fields      []protoMessageField
	Oneofs      []protoOneof
}

type protoOneof struct {
	Name     string
	Member   string
	EnumType string
	NotSet   string
	Fields   []protoMessageField
}

type protoEnum struct {
	EnumName string
	Options  []protoEnumValue
}

type protoEnumValue struct {
	Name   string
	Number int32
}

func enumValues(enum *descriptorpb.EnumDescriptorProto) []protoEnumValue {
	var values []protoEnumValue
	for _, val := range enum.GetValue() {
		values = append(values, protoEnumValue{Name: val.GetName(), Number: val.GetNumber()})
	}
	return values
}

type protoMessageField struct {
	FieldName           string
	ProtoTypeName       string
	GodotType           string
	GodotClassName      string
	InnerGodotType      string
	InnerGodotClassName string
	IsCustomType        bool
	IsInnerCustomType   bool
	IsRepeated          bool
	IsEnum              bool
	IsMap               bool
	IsTypedArray        bool
	Description         string
	OneofName           string
	Number              int32
	EnumOptionsString   string

	// set by finishField
	Name        string
	CppType     string
	VariantType string
	Hint        string
	HintString  string
	DocType     string
	CaseConst   string
	Tag         string
	OneofMember string
	OneofNotSet string
}

func finishField(f *protoMessageField, msg *protoMessage, oneof *protoOneof) {
	f.Name = xstrings.ToSnakeCase(f.FieldName)
	f.CppType = f.GodotType
	if f.IsCustomType {
		f.CppType = fmt.Sprintf("godot::Ref<%s>", f.GodotType)
	}
	f.VariantType = godotVariantType(f.GodotType, f.IsCustomType, f.IsEnum)
	f.DocType = godotDocType(f.GodotType, f.IsCustomType, f.IsEnum)

	f.Hint, f.HintString = "godot::PROPERTY_HINT_NONE", ""
	switch {
	case f.IsCustomType:
		f.Hint, f.HintString = "godot::PROPERTY_HINT_RESOURCE_TYPE", f.GodotClassName
	case f.IsEnum && f.EnumOptionsString != "":
		f.Hint, f.HintString = "godot::PROPERTY_HINT_ENUM", f.EnumOptionsString
	case f.IsRepeated && f.IsInnerCustomType:
		f.Hint, f.HintString = "godot::PROPERTY_HINT_ARRAY_TYPE", f.InnerGodotClassName
	case f.IsRepeated && f.IsTypedArray:
		f.Hint, f.HintString = "godot::PROPERTY_HINT_ARRAY_TYPE", godotArrayElementHint(f.InnerGodotType)
	}

	if oneof != nil {
		f.OneofName = oneof.Name
		f.OneofMember = oneof.Member
		f.OneofNotSet = oneof.NotSet
		f.CaseConst = "k" + toPascalCase(f.FieldName)
		f.Tag = fmt.Sprintf("%s_%s_tag", msg.StructName, f.FieldName)
	}
}

func NewCodeGenerator(logger *slog.Logger, destinationDirectoryPath, extensionName string) (*CodeGenerator, error) {
	tmpl, err := template.New("gdbuf").Option("missingkey=error").ParseFS(templatesFS, "templates/**/*.tmpl", "templates/**/**/*.tmpl")
	if err != nil {
		return nil, fmt.Errorf("could not parse code generation file templates: %w", err)
	}

	return &CodeGenerator{
		logger:                   logger,
		destinationDirectoryPath: destinationDirectoryPath,
		templates:                tmpl,
		extensionName:            strings.ReplaceAll(extensionName, "-", "_"),
	}, nil
}

// nanopbType turns a fully qualified proto name (".pkg.Outer.Inner") into nanopb's C typedef ("pkg_Outer_Inner").
func nanopbType(protoType string) string {
	return strings.ReplaceAll(strings.TrimPrefix(protoType, "."), ".", "_")
}

// godotBuiltinName maps a C++ member type to the Godot type name, or "" when there is none.
func godotBuiltinName(godotType string) string {
	switch godotType {
	case "bool":
		return "bool"
	case "int32_t", "int64_t", "uint32_t", "uint64_t":
		return "int"
	case "float", "double":
		return "float"
	case "godot::String":
		return "String"
	case "godot::PackedByteArray":
		return "PackedByteArray"
	case "godot::PackedStringArray":
		return "PackedStringArray"
	case "godot::Dictionary":
		return "Dictionary"
	case "godot::Array":
		return "Array"
	}
	if strings.HasPrefix(godotType, "godot::TypedArray<") {
		return "Array"
	}
	return ""
}

func godotVariantType(godotType string, isCustom bool, isEnum bool) string {
	switch {
	case isEnum:
		return "godot::Variant::INT"
	case isCustom:
		return "godot::Variant::OBJECT"
	}
	switch name := godotBuiltinName(godotType); name {
	case "":
		return "godot::Variant::NIL"
	case "PackedByteArray":
		return "godot::Variant::PACKED_BYTE_ARRAY"
	case "PackedStringArray":
		return "godot::Variant::PACKED_STRING_ARRAY"
	default:
		return "godot::Variant::" + strings.ToUpper(name)
	}
}

func godotDocType(godotType string, isCustom bool, isEnum bool) string {
	switch {
	case isEnum:
		return "int"
	case isCustom:
		parts := strings.Split(godotType, "::")
		return parts[len(parts)-1]
	}
	if name := godotBuiltinName(godotType); name != "" {
		return name
	}
	return "Variant"
}

func godotArrayElementHint(godotType string) string {
	if godotType == "godot::Array" {
		return ""
	}
	return godotBuiltinName(godotType)
}

func (cg *CodeGenerator) GenerateCode(fileDescriptorSet []*descriptorpb.FileDescriptorProto) error {
	// first extract all of the data needed
	protoData, err := cg.extractProtoData(fileDescriptorSet)
	if err != nil {
		return fmt.Errorf("problem extracting proto data: %w", err)
	}

	templateData := templateData{
		GDExtensionName: cg.extensionName,
		ProtoData:       *protoData,
		GlobalEnums:     cg.extractGlobalEnums(fileDescriptorSet),
	}

	oneTimeTemplates := map[string]string{
		"SConstruct.tmpl":               "SConstruct",
		"gde-protobuf.gdextension.tmpl": "out/gde-protobuf.gdextension",
		"register_types.h.tmpl":         "src/register_types.h",
		"register_types.cpp.tmpl":       "src/register_types.cpp",
		"messages.h.tmpl":               "src/messages.h",
		"global_class.h.tmpl":           "src/global_class.h",
		"global_class.cpp.tmpl":         "src/global_class.cpp",
	}

	for templateName, outputPath := range oneTimeTemplates {
		err := os.MkdirAll(filepath.Dir(filepath.Join(cg.destinationDirectoryPath, outputPath)), 0755)
		if err != nil {
			return fmt.Errorf("could not make template output directory: %w", err)
		}
		if err := cg.executeTemplate(templateName, filepath.Join(cg.destinationDirectoryPath, outputPath), templateData); err != nil {
			return fmt.Errorf("could not execute template %s: %w", templateName, err)
		}
	}

	// generate for each proto file
	for _, file := range templateData.ProtoData.Files {
		cg.logger.Info("processing file", "name", file.ProtoPath)
		thisProtoFileTemplates := map[string]string{
			"resource.h.tmpl":   "src/" + file.PathNoExt + ".h",
			"resource.cpp.tmpl": "src/" + file.PathNoExt + ".cpp",
		}
		for templateName, outputPath := range thisProtoFileTemplates {
			err := os.MkdirAll(filepath.Dir(filepath.Join(cg.destinationDirectoryPath, outputPath)), 0755)
			if err != nil {
				return fmt.Errorf("could not make template output directory: %w", err)
			}
			if err := cg.executeTemplate(templateName, filepath.Join(cg.destinationDirectoryPath, outputPath), file); err != nil {
				return fmt.Errorf("could not execute template %s: %w", templateName, err)
			}
		}

		for _, msg := range file.Messages {
			className := msg.ClassName
			outputPath := filepath.Join(cg.destinationDirectoryPath, "doc_classes", className+".xml")
			if err := cg.executeTemplate("class_doc.xml.tmpl", outputPath, msg); err != nil {
				return fmt.Errorf("could not execute template class_doc.xml.tmpl for message %s: %w", msg.MessageName, err)
			}
		}
	}

	// Generate documentation for the global extension class
	globalClassMsg := protoMessage{
		ClassName:   cg.extensionName,
		Description: "Global extension class containing utilities and enums.",
	}
	outputPath := filepath.Join(cg.destinationDirectoryPath, "doc_classes", globalClassMsg.ClassName+".xml")
	if err := cg.executeTemplate("class_doc.xml.tmpl", outputPath, globalClassMsg); err != nil {
		return fmt.Errorf("could not execute template class_doc.xml.tmpl for global class: %w", err)
	}

	return nil
}

func traverseEnums(enums []*descriptorpb.EnumDescriptorProto, currentPrefix string, globalEnumsOptionsMap map[string]string) {
	for _, enum := range enums {
		var options []string
		for _, val := range enum.GetValue() {
			// Godot's PROPERTY_HINT_ENUM takes "NAME:value"; without the value it numbers by index
			options = append(options, fmt.Sprintf("%s:%d", val.GetName(), val.GetNumber()))
		}
		globalEnumsOptionsMap[currentPrefix+enum.GetName()] = strings.Join(options, ",")
	}
}

func traverseNestedEnums(msgs []*descriptorpb.DescriptorProto, currentPrefix string, globalEnumsOptionsMap map[string]string) {
	for _, msg := range msgs {
		msgPrefix := currentPrefix + msg.GetName() + "."
		traverseEnums(msg.GetEnumType(), msgPrefix, globalEnumsOptionsMap)
		traverseNestedEnums(msg.GetNestedType(), msgPrefix, globalEnumsOptionsMap)
	}
}

func (cg *CodeGenerator) extractProtoData(fileDescriptorSet []*descriptorpb.FileDescriptorProto) (*protoData, error) {
	var protoData protoData
	// one loop through to get a mapping of filename and message name (recursive)
	var protoFileToDeclaredMessageNames map[string][]string = make(map[string][]string)
	var protoFileToDeclaredEnumNames map[string][]string = make(map[string][]string)
	var allMessageDescriptors map[string]*descriptorpb.DescriptorProto = make(map[string]*descriptorpb.DescriptorProto)
	var typeToGodotName map[string]string = make(map[string]string)

	for _, file := range fileDescriptorSet {
		pkg := file.GetPackage()
		prefix := "."
		if pkg != "" {
			prefix = "." + pkg + "."
		}

		// Recursive message traversal
		var traverseMsgs func(msgs []*descriptorpb.DescriptorProto, currentPrefix string)
		traverseMsgs = func(msgs []*descriptorpb.DescriptorProto, currentPrefix string) {
			for _, msg := range msgs {
				fullName := currentPrefix + msg.GetName()
				protoFileToDeclaredMessageNames[file.GetName()] = append(protoFileToDeclaredMessageNames[file.GetName()], fullName)
				allMessageDescriptors[fullName] = msg

				// Construct Godot Name
				// Remove package prefix
				shortName := strings.TrimPrefix(fullName, prefix)
				// Replace . with _
				godotName := strings.ReplaceAll(shortName, ".", "_")
				typeToGodotName[fullName] = godotName

				traverseMsgs(msg.GetNestedType(), fullName+".")

				// Also traverse Nested Enums
				for _, enum := range msg.GetEnumType() {
					enumFullName := fullName + "." + enum.GetName()
					protoFileToDeclaredEnumNames[file.GetName()] = append(protoFileToDeclaredEnumNames[file.GetName()], enumFullName)
				}
			}
		}
		traverseMsgs(file.GetMessageType(), prefix)

		// Top level enums
		for _, enum := range file.GetEnumType() {
			fullName := prefix + enum.GetName()
			protoFileToDeclaredEnumNames[file.GetName()] = append(protoFileToDeclaredEnumNames[file.GetName()], fullName)
		}
	}

	globalEnumsMap := make(map[string]bool)
	globalEnumsOptionsMap := make(map[string]string)

	for _, file := range fileDescriptorSet {
		pkg := file.GetPackage()
		prefix := "."
		if pkg != "" {
			prefix = "." + pkg + "."
		}

		// Global enums
		for _, enum := range file.GetEnumType() {
			globalEnumsMap[prefix+enum.GetName()] = true
		}

		traverseEnums(file.GetEnumType(), prefix, globalEnumsOptionsMap)
		traverseNestedEnums(file.GetMessageType(), prefix, globalEnumsOptionsMap)
	}

	// now for the real deal
	for _, file := range fileDescriptorSet {
		var protoFile protoFile
		protoFile.ProtoPath = file.GetName()
		protoFile.PathNoExt = strings.TrimSuffix(file.GetName(), ".proto")
		protoFile.BaseName = path.Base(protoFile.PathNoExt)
		protoFile.Dir = path.Dir(file.GetName())
		protoFile.Namespace = xstrings.ToSnakeCase(protoFile.BaseName)
		protoFile.PackageName = file.GetPackage()

		addDependency := func(godotType, srcFile string) {
			if srcFile == "" || srcFile == "google::protobuf" || srcFile == protoFile.ProtoPath {
				return
			}
			headerPath := strings.TrimSuffix(srcFile, ".proto") + ".h"
			if !slices.Contains(protoFile.Dependencies, headerPath) {
				protoFile.Dependencies = append(protoFile.Dependencies, headerPath)
			}
			if i := strings.LastIndex(godotType, "::"); i != -1 {
				fd := ForwardDecl{Namespace: godotType[:i], ClassName: godotType[i+2:]}
				if !slices.Contains(protoFile.ForwardDecls, fd) {
					protoFile.ForwardDecls = append(protoFile.ForwardDecls, fd)
				}
			}
		}
		cg.logger.Info("processing proto file", "name", file.GetName(), "package", protoFile.PackageName)

		pkg := file.GetPackage()
		prefix := "."
		if pkg != "" {
			prefix = "." + pkg + "."
		}

		// Recursive generation
		var messagesToGenerate []protoMessage
		var traverseGen func(msgs []*descriptorpb.DescriptorProto, currentPrefix string, path []int32) error
		traverseGen = func(msgs []*descriptorpb.DescriptorProto, currentPrefix string, path []int32) error {
			for msgIndex, msg := range msgs {
				if msg.GetOptions().GetMapEntry() {
					continue
				}

				fullName := currentPrefix + msg.GetName()
				godotName := typeToGodotName[fullName]

				var protoMessage protoMessage
				protoMessage.MessageName = godotName
				protoMessage.ClassName = godotName
				currentPath := append(slices.Clone(path), int32(msgIndex))
				protoMessage.Description = getComments(file.GetSourceCodeInfo(), currentPath)
				protoMessage.StructName = nanopbType(fullName)

				// proto3 `optional` fields live in synthetic oneofs, which are not real oneofs
				synthetic := make([]bool, len(msg.GetOneofDecl()))
				for _, field := range msg.GetField() {
					if field.OneofIndex != nil && field.GetProto3Optional() {
						synthetic[field.GetOneofIndex()] = true
					}
				}
				oneofByDecl := make([]int, len(msg.GetOneofDecl()))
				for i, oneof := range msg.GetOneofDecl() {
					oneofByDecl[i] = -1
					if synthetic[i] {
						continue
					}
					name := sanitizeCpp(oneof.GetName())
					snake := xstrings.ToSnakeCase(name)
					oneofByDecl[i] = len(protoMessage.Oneofs)
					protoMessage.Oneofs = append(protoMessage.Oneofs, protoOneof{
						Name:     name,
						Member:   snake + "_case",
						EnumType: toPascalCase(name) + "Case",
						NotSet:   strings.ToUpper(snake) + "_NOT_SET",
					})
				}

				for fieldIndex, field := range msg.GetField() {
					var protoMessageField protoMessageField
					protoMessageField.FieldName = sanitizeCpp(field.GetName())
					protoMessageField.ProtoTypeName = field.GetTypeName()

					protoMessageField.Number = field.GetNumber()
					fieldPath := append(slices.Clone(currentPath), 2, int32(fieldIndex))
					protoMessageField.Description = getComments(file.GetSourceCodeInfo(), fieldPath)

					oneofIdx := -1
					if field.OneofIndex != nil {
						oneofIdx = oneofByDecl[field.GetOneofIndex()]
					}

					// Add default descriptions for WKTs
					if protoMessageField.ProtoTypeName == ".google.protobuf.Timestamp" {
						if protoMessageField.Description != "" {
							protoMessageField.Description += "\n"
						}
						protoMessageField.Description += "Note: This field is a Google Protobuf Timestamp. In Godot, it is represented as an int64 (Unix timestamp in milliseconds)."
					} else if protoMessageField.ProtoTypeName == ".google.protobuf.Duration" {
						if protoMessageField.Description != "" {
							protoMessageField.Description += "\n"
						}
						protoMessageField.Description += "Note: This field is a Google Protobuf Duration. In Godot, it is represented as a double (seconds)."
					} else if protoMessageField.ProtoTypeName == ".google.protobuf.Struct" {
						if protoMessageField.Description != "" {
							protoMessageField.Description += "\n"
						}
						protoMessageField.Description += "Note: This field is a Google Protobuf Struct. In Godot, it is represented as a Dictionary."
					}

					godotType, godotClassName, isCustom, isEnum, srcFile, err := resolveGodotType(field, protoFile.ProtoPath, protoFileToDeclaredMessageNames, protoFileToDeclaredEnumNames, allMessageDescriptors, typeToGodotName, globalEnumsMap, cg.extensionName)
					if err != nil {
						return fmt.Errorf("could not resolve godot type: %w", err)
					}

					if isCustom {
						addDependency(godotType, srcFile)
					}

					protoMessageField.IsCustomType = isCustom
					protoMessageField.IsEnum = isEnum
					if isEnum {
						protoMessageField.EnumOptionsString = globalEnumsOptionsMap[field.GetTypeName()]
					}

					protoMessageField.InnerGodotType = godotType
					protoMessageField.InnerGodotClassName = godotClassName
					protoMessageField.IsInnerCustomType = protoMessageField.IsCustomType

					if desc, ok := allMessageDescriptors[field.GetTypeName()]; ok && desc.GetOptions().GetMapEntry() {
						protoMessageField.IsMap = true
						var keyField, valueField *descriptorpb.FieldDescriptorProto
						for _, f := range desc.GetField() {
							if f.GetNumber() == 1 {
								keyField = f
							}
							if f.GetNumber() == 2 {
								valueField = f
							}
						}
						if _, _, _, _, _, err := resolveGodotType(keyField, protoFile.ProtoPath, protoFileToDeclaredMessageNames, protoFileToDeclaredEnumNames, allMessageDescriptors, typeToGodotName, globalEnumsMap, cg.extensionName); err != nil {
							return fmt.Errorf("could not resolve map key type: %w", err)
						}
						valType, _, valCustom, _, valSrcFile, err := resolveGodotType(valueField, protoFile.ProtoPath, protoFileToDeclaredMessageNames, protoFileToDeclaredEnumNames, allMessageDescriptors, typeToGodotName, globalEnumsMap, cg.extensionName)
						if err != nil {
							return fmt.Errorf("could not resolve map value type: %w", err)
						}
						if valCustom {
							addDependency(valType, valSrcFile)
						}
						protoMessageField.GodotType = "godot::Dictionary"
						protoMessageField.GodotClassName = "Dictionary"
						protoMessageField.IsCustomType = false
						protoMessageField.IsInnerCustomType = false
					} else {
						protoMessageField.GodotType = godotType
						protoMessageField.GodotClassName = godotClassName
						if field.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
							protoMessageField.IsRepeated = true
							if protoMessageField.IsInnerCustomType {
								protoMessageField.GodotType = fmt.Sprintf("godot::TypedArray<%s>", protoMessageField.InnerGodotType)
								protoMessageField.IsTypedArray = true
							} else {
								switch protoMessageField.InnerGodotType {
								case "bool":
									protoMessageField.GodotType = "godot::TypedArray<bool>"
									protoMessageField.IsTypedArray = true
								case "int32_t", "int64_t", "uint32_t", "uint64_t":
									protoMessageField.GodotType = "godot::TypedArray<int64_t>"
									protoMessageField.IsTypedArray = true
								case "float", "double":
									protoMessageField.GodotType = "godot::TypedArray<double>"
									protoMessageField.IsTypedArray = true
								case "godot::String":
									protoMessageField.GodotType = "godot::TypedArray<godot::String>"
									protoMessageField.IsTypedArray = true
								default:
									protoMessageField.GodotType = "godot::Array"
								}
							}
							protoMessageField.GodotClassName = "Array"
							protoMessageField.IsCustomType = false
							protoMessageField.IsEnum = false
						}
					}

					if oneofIdx >= 0 {
						oneof := &protoMessage.Oneofs[oneofIdx]
						finishField(&protoMessageField, &protoMessage, oneof)
						oneof.Fields = append(oneof.Fields, protoMessageField)
					} else {
						finishField(&protoMessageField, &protoMessage, nil)
					}

					protoMessage.Fields = append(protoMessage.Fields, protoMessageField)
				}
				cg.logger.Debug("Generated message", "name", protoMessage.MessageName, "fields", len(protoMessage.Fields))
				messagesToGenerate = append(messagesToGenerate, protoMessage)

				// Recurse for nested types. NestedType is field 3.
				nestedPath := append(slices.Clone(currentPath), 3)
				if err := traverseGen(msg.GetNestedType(), fullName+".", nestedPath); err != nil {
					return err
				}
			}
			return nil
		}

		// MessageType is field 4 in FileDescriptorProto
		if err := traverseGen(file.GetMessageType(), prefix, []int32{4}); err != nil {
			return nil, err
		}
		protoFile.Messages = messagesToGenerate

		protoData.Files = append(protoData.Files, protoFile)
	}
	return &protoData, nil
}

func getComments(sc *descriptorpb.SourceCodeInfo, path []int32) string {
	if sc == nil {
		return ""
	}
	for _, loc := range sc.GetLocation() {
		if slices.Equal(loc.Path, path) {
			c := strings.TrimSpace(loc.GetLeadingComments())
			if c == "" {
				c = strings.TrimSpace(loc.GetTrailingComments())
			}
			// Clean up comments: Remove leading * or // if present (protoc usually handles this but sometimes...)
			// Actually GetLeadingComments usually returns the raw comment content.
			return strings.TrimSpace(c)
		}
	}
	return ""
}

func (cg *CodeGenerator) extractGlobalEnums(fileDescriptorSet []*descriptorpb.FileDescriptorProto) []protoEnum {
	var globalEnums []protoEnum
	for _, file := range fileDescriptorSet {
		for _, enum := range file.GetEnumType() {
			var protoEnum protoEnum
			protoEnum.EnumName = enum.GetName()
			protoEnum.Options = enumValues(enum)
			globalEnums = append(globalEnums, protoEnum)
		}
	}
	return globalEnums
}

func sanitizeCpp(name string) string {
	keywords := map[string]bool{
		"alignas": true, "alignof": true, "and": true, "and_eq": true, "asm": true,
		"atomic_cancel": true, "atomic_commit": true, "atomic_noexcept": true, "auto": true,
		"bitand": true, "bitor": true, "bool": true, "break": true, "case": true,
		"catch": true, "char": true, "char8_t": true, "char16_t": true, "char32_t": true,
		"class": true, "compl": true, "concept": true, "const": true, "consteval": true,
		"constexpr": true, "constinit": true, "const_cast": true, "continue": true,
		"co_await": true, "co_return": true, "co_yield": true, "decltype": true,
		"default": true, "delete": true, "do": true, "double": true, "dynamic_cast": true,
		"else": true, "enum": true, "explicit": true, "export": true, "extern": true,
		"false": true, "float": true, "for": true, "friend": true, "goto": true,
		"if": true, "inline": true, "int": true, "long": true, "mutable": true,
		"namespace": true, "new": true, "noexcept": true, "not": true, "not_eq": true,
		"nullptr": true, "operator": true, "or": true, "or_eq": true, "private": true,
		"protected": true, "public": true, "register": true, "reinterpret_cast": true,
		"requires": true, "return": true, "short": true, "signed": true, "sizeof": true,
		"static": true, "static_assert": true, "static_cast": true, "struct": true,
		"switch": true, "synchronized": true, "template": true, "this": true,
		"thread_local": true, "throw": true, "true": true, "try": true, "typedef": true,
		"typeid": true, "typename": true, "union": true, "unsigned": true, "using": true,
		"virtual": true, "void": true, "volatile": true, "wchar_t": true, "while": true,
		"xor": true, "xor_eq": true,
	}
	if keywords[name] {
		return name + "_"
	}
	return name
}
