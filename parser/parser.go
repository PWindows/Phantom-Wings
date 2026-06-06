package parser

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/apex/log"
	"github.com/beevik/etree"
	"github.com/buger/jsonparser"
	"github.com/goccy/go-json"
	"github.com/icza/dyno"
	"github.com/magiconair/properties"
	"github.com/pelletier/go-toml/v2"
	"github.com/tidwall/pretty"
	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v3"

	"github.com/pwindows/phantom-wings/config"
)

const (
	File       = "file"
	Yaml       = "yaml"
	Properties = "properties"
	Ini        = "ini"
	Json       = "json"
	Xml        = "xml"
	Toml       = "toml"
)

type ReplaceValue struct {
	value     []byte
	valueType jsonparser.ValueType
}

func (cv *ReplaceValue) Value() []byte {
	return cv.value
}

func (cv *ReplaceValue) Type() jsonparser.ValueType {
	return cv.valueType
}

func (cv *ReplaceValue) String() string {
	switch cv.Type() {
	case jsonparser.String:
		str, err := jsonparser.ParseString(cv.value)
		if err != nil {
			panic(errors.Wrap(err, "parser: could not parse value"))
		}
		return str
	case jsonparser.Null:
		return "<nil>"
	case jsonparser.Boolean:
		return string(cv.value)
	case jsonparser.Number:
		return string(cv.value)
	default:
		return "<invalid>"
	}
}

func (cv *ReplaceValue) Bytes() []byte {
	switch cv.Type() {
	case jsonparser.String:
		var stackbuf [64]byte
		bU, err := jsonparser.Unescape(cv.value, stackbuf[:])
		if err != nil {
			panic(errors.Wrap(err, "parser: could not parse value"))
		}
		return bU
	case jsonparser.Null:
		return []byte("<nil>")
	case jsonparser.Boolean:
		return cv.value
	case jsonparser.Number:
		return cv.value
	default:
		return []byte("<invalid>")
	}
}

type ConfigurationParser string

func (cp ConfigurationParser) String() string {
	return string(cp)
}

type ConfigurationFile struct {
	FileName        string                         `json:"file"`
	Parser          ConfigurationParser            `json:"parser"`
	Replace         []ConfigurationFileReplacement `json:"replace"`
	AllowCreateFile bool                           `json:"create_file"`
	configuration   []byte
}

func (f *ConfigurationFile) UnmarshalJSON(data []byte) error {
	var m map[string]*json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}

	fileRaw, ok := m["file"]
	if !ok || fileRaw == nil {
		return errors.New("parser: configuration file missing required 'file' key")
	}
	if err := json.Unmarshal(*fileRaw, &f.FileName); err != nil {
		return err
	}

	parserRaw, ok := m["parser"]
	if !ok || parserRaw == nil {
		return errors.New("parser: configuration file missing required 'parser' key")
	}
	if err := json.Unmarshal(*parserRaw, &f.Parser); err != nil {
		return err
	}

	f.Replace = []ConfigurationFileReplacement{}
	if replaceRaw, ok := m["replace"]; ok && replaceRaw != nil {
		if err := json.Unmarshal(*replaceRaw, &f.Replace); err != nil {
			log.WithField("file", f.FileName).WithField("error", err).Warn("failed to unmarshal configuration file replacement")
		}
	}

	if val, exists := m["create_file"]; exists && val != nil {
		if err := json.Unmarshal(*val, &f.AllowCreateFile); err != nil {
			log.WithField("file", f.FileName).WithField("error", err).Warn("create_file unmarshal failed")
			f.AllowCreateFile = true
		}
	} else {
		log.WithField("file", f.FileName).Debug("create_file not specified assumed true")
		f.AllowCreateFile = true
	}

	return nil
}

type ConfigurationFileReplacement struct {
	Match       string       `json:"match"`
	IfValue     string       `json:"if_value"`
	ReplaceWith ReplaceValue `json:"replace_with"`
}

func (cfr *ConfigurationFileReplacement) UnmarshalJSON(data []byte) error {
	m, err := jsonparser.GetString(data, "match")
	if err != nil {
		return err
	}
	cfr.Match = m

	iv, err := jsonparser.GetString(data, "if_value")
	if err != nil && err != jsonparser.KeyPathNotFoundError {
		return err
	}
	cfr.IfValue = iv

	rw, dt, _, err := jsonparser.Get(data, "replace_with")
	if err != nil {
		if err != jsonparser.KeyPathNotFoundError {
			return err
		}
		rw, dt, _, err = jsonparser.Get(data, "value")
		if err != nil {
			return err
		}
	}

	cfr.ReplaceWith = ReplaceValue{
		value:     rw,
		valueType: dt,
	}

	return nil
}

type templatableConfig struct {
	Docker struct {
		Interface string `json:"interface"`
		Network   struct {
			Interface string `json:"interface"`
		} `json:"network"`
	} `json:"docker"`
}

func newTemplatableConfig(c *config.Configuration) templatableConfig {
	var t templatableConfig
	t.Docker.Interface = c.Docker.Network.Interface
	t.Docker.Network.Interface = c.Docker.Network.Interface
	return t
}

func (f *ConfigurationFile) Parse(file *os.File) error {
	if mb, err := json.Marshal(newTemplatableConfig(config.Get())); err != nil {
		return err
	} else {
		f.configuration = mb
	}

	var err error
	switch f.Parser {
	case Properties:
		err = f.parsePropertiesFile(file)
	case File:
		err = f.parseTextFile(file)
	case Yaml, "yml":
		err = f.parseYamlFile(file)
	case Json:
		err = f.parseJsonFile(file)
	case Ini:
		err = f.parseIniFile(file)
	case Xml:
		err = f.parseXmlFile(file)
	case Toml:
		err = f.parseTomlFile(file)
	}
	return err
}

func (f *ConfigurationFile) parseXmlFile(file *os.File) error {
	doc := etree.NewDocument()
	if _, err := doc.ReadFrom(file); err != nil {
		return err
	}

	if doc.Root() == nil {
		doc.CreateProcInst("xml", `version="1.0" encoding="utf-8"`)
	}

	for i, replacement := range f.Replace {
		value, err := f.LookupConfigurationValue(replacement)
		if err != nil {
			return err
		}

		if i == 0 && doc.Root() == nil {
			parts := strings.SplitN(replacement.Match, ".", 2)
			doc.SetRoot(doc.CreateElement(parts[0]))
		}

		path := "./" + strings.Replace(replacement.Match, ".", "/", -1)

		if !strings.Contains(path, "*") {
			parts := strings.Split(replacement.Match, ".")
			element := doc.Root()
			for _, tag := range parts[1:] {
				if e := element.FindElement(tag); e == nil {
					element = element.CreateElement(tag)
				} else {
					element = e
				}
			}
		}

		for _, element := range doc.FindElements(path) {
			if xmlValueMatchRegex.MatchString(value) {
				k := xmlValueMatchRegex.ReplaceAllString(value, "$1")
				v := xmlValueMatchRegex.ReplaceAllString(value, "$2")
				element.RemoveAttr(k)
				element.CreateAttr(k, v)
			} else {
				element.SetText(value)
			}
		}
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}

	doc.Indent(2)
	if _, err := doc.WriteTo(file); err != nil {
		return err
	}
	return nil
}

func (f *ConfigurationFile) parseIniFile(file *os.File) error {
	cfg, err := ini.Load(io.NopCloser(file))
	if err != nil {
		return err
	}

	ini.PrettyFormat = false
	ini.PrettyEqual = true

	for _, replacement := range f.Replace {
		var (
			path         []string
			bracketDepth int
			v            []int32
		)
		for _, c := range replacement.Match {
			switch c {
			case '[':
				bracketDepth++
			case ']':
				bracketDepth--
			case '.':
				if bracketDepth > 0 || len(path) == 1 {
					v = append(v, c)
					continue
				}
				path = append(path, string(v))
				v = v[:0]
			default:
				v = append(v, c)
			}
		}
		path = append(path, string(v))

		value, err := f.LookupConfigurationValue(replacement)
		if err != nil {
			return err
		}

		k := path[0]
		s := cfg.Section("")
		if len(path) == 2 {
			k = path[1]
			s = cfg.Section(path[0])
		}

		if s == nil {
			s, err = cfg.NewSection(path[0])
			if err != nil {
				return err
			}
		}

		if s.HasKey(k) {
			s.Key(k).SetValue(value)
		} else {
			if _, err := s.NewKey(k, value); err != nil {
				return err
			}
		}
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := cfg.WriteTo(file); err != nil {
		return err
	}
	return nil
}

func (f *ConfigurationFile) parseJsonFile(file *os.File) error {
	b, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	data, err := f.IterateOverJson(b)
	if err != nil {
		return err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}

	prettified := pretty.PrettyOptions(data, &pretty.Options{
		Width:    80,
		Prefix:   "",
		Indent:   "  ",
		SortKeys: false,
	})

	if _, err := io.Copy(file, bytes.NewReader(prettified)); err != nil {
		return errors.Wrap(err, "parser: failed to write properties file to disk")
	}
	return nil
}

func (f *ConfigurationFile) parseYamlFile(file *os.File) error {
	b, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	i := make(map[string]interface{})
	if err := yaml.Unmarshal(b, &i); err != nil {
		return err
	}

	jsonBytes, err := json.Marshal(dyno.ConvertMapI2MapS(i))
	if err != nil {
		return err
	}

	data, err := f.IterateOverJson(jsonBytes)
	if err != nil {
		return err
	}

	var jsonData interface{}
	yamlDecoder := json.NewDecoder(bytes.NewReader(data))
	yamlDecoder.UseNumber()
	if err := yamlDecoder.Decode(&jsonData); err != nil {
		return err
	}
	jsonData = normalizeYamlTypes(jsonData)

	marshaled, err := yaml.Marshal(jsonData)
	if err != nil {
		return err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := io.Copy(file, bytes.NewReader(marshaled)); err != nil {
		return errors.Wrap(err, "parser: failed to write properties file to disk")
	}
	return nil
}

func (f *ConfigurationFile) parseTomlFile(file *os.File) error {
	b, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	i := make(map[string]interface{})
	if err := toml.Unmarshal(b, &i); err != nil {
		return err
	}

	jsonBytes, err := json.Marshal(dyno.ConvertMapI2MapS(i))
	if err != nil {
		return err
	}

	data, err := f.IterateOverJson(jsonBytes)
	if err != nil {
		return err
	}

	var jsonData interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&jsonData); err != nil {
		return err
	}
	jsonData = normalizeTomlTypes(jsonData)

	marshaled, err := toml.Marshal(jsonData)
	if err != nil {
		return err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := io.Copy(file, bytes.NewReader(marshaled)); err != nil {
		return errors.Wrap(err, "parser: failed to write toml file to disk")
	}
	return nil
}

func normalizeYamlTypes(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, item := range typed {
			typed[key] = normalizeYamlTypes(item)
		}
		return typed
	case []interface{}:
		for i := range typed {
			typed[i] = normalizeYamlTypes(typed[i])
		}
		return typed
	case json.Number:
		s := typed.String()
		if strings.ContainsAny(s, ".eE") {
			if floatVal, err := typed.Float64(); err == nil {
				return floatVal
			}
			return s
		}
		if intVal, err := typed.Int64(); err == nil {
			return intVal
		}
		if floatVal, err := typed.Float64(); err == nil {
			return floatVal
		}
		return s
	default:
		return value
	}
}

func normalizeTomlTypes(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, item := range typed {
			typed[key] = normalizeTomlTypes(item)
		}
		return typed
	case []interface{}:
		for i := range typed {
			typed[i] = normalizeTomlTypes(typed[i])
		}
		return typed
	case json.Number:
		if intVal, err := typed.Int64(); err == nil {
			return intVal
		}
		if floatVal, err := typed.Float64(); err == nil {
			return floatVal
		}
		return typed.String()
	case string:
		if timeVal, err := time.Parse(time.RFC3339Nano, typed); err == nil {
			return timeVal
		}
		if timeVal, err := time.Parse(time.RFC3339, typed); err == nil {
			return timeVal
		}
		return typed
	default:
		return value
	}
}

func (f *ConfigurationFile) parseTextFile(file *os.File) error {
	b := bytes.NewBuffer(nil)
	s := bufio.NewScanner(file)
	var replaced bool
	for s.Scan() {
		line := s.Bytes()
		replaced = false
		for _, replace := range f.Replace {
			if !bytes.HasPrefix(line, []byte(replace.Match)) {
				continue
			}
			if replace.IfValue != "" {
				remainder := bytes.TrimRight(bytes.TrimPrefix(line, []byte(replace.Match)), "\r\n")
				if string(remainder) != replace.IfValue {
					continue
				}
			}
			b.Write(replace.ReplaceWith.Bytes())
			replaced = true
		}
		if !replaced {
			b.Write(line)
		}
		b.WriteByte('\n')
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := io.Copy(file, b); err != nil {
		return errors.Wrap(err, "parser: failed to write properties file to disk")
	}
	return nil
}

func (f *ConfigurationFile) parsePropertiesFile(file *os.File) error {
	b, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	s := bytes.NewBuffer(nil)
	scanner := bufio.NewScanner(bytes.NewReader(b))
	for scanner.Scan() {
		text := scanner.Bytes()
		if len(text) > 0 && text[0] != '#' {
			break
		}
		s.Write(text)
		s.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return errors.WithStackIf(err)
	}

	p, err := properties.Load(b, properties.UTF8)
	if err != nil {
		return errors.Wrap(err, "parser: could not load properties file for configuration update")
	}

	for _, replace := range f.Replace {
		data, err := f.LookupConfigurationValue(replace)
		if err != nil {
			return errors.Wrap(err, "parser: failed to lookup configuration value")
		}

		v, ok := p.Get(replace.Match)
		if replace.IfValue != "" && (!ok || (ok && v != replace.IfValue)) {
			continue
		}

		if _, _, err := p.Set(replace.Match, data); err != nil {
			return errors.Wrap(err, "parser: failed to set replacement value")
		}
	}

	for _, key := range p.Keys() {
		value, ok := p.Get(key)
		if !ok {
			continue
		}
		s.WriteString(key + "=" + strings.Trim(strconv.QuoteToASCII(value), "\"") + "\n")
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := io.Copy(file, s); err != nil {
		return errors.Wrap(err, "parser: failed to write properties file to disk")
	}
	return nil
}