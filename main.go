package configloader

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/golang-collections/collections/queue"
)

type currentField struct {
	original reflect.StructField
	value    reflect.Value
	name     string
	index    int
}

// Hook is something that loads data from a source
// and stores it into your configuration struct
// (here is an interface)
type Hook interface {
	run(interface{})
}

// ConfigLoader loads data into a target (a config struct).
// Data can come from different hooks.
type ConfigLoader struct {
	hooks  *queue.Queue
	target interface{}
}

// NewConfigLoaderFor creates a ConfigLoader for a target
// struct, where data will be loaded. You should pass
// a pointer to a empty struct instance.
func NewConfigLoaderFor(target interface{}) *ConfigLoader {
	return &ConfigLoader{
		hooks:  queue.New(),
		target: target,
	}
}

// AddHook adds a new source to load data from.
func (loader *ConfigLoader) AddHook(hook Hook) *ConfigLoader {
	loader.hooks.Enqueue(hook)
	return loader
}

// Retrieve loaded struct. It'll return a pointer to your struct.
func (loaded ConfigLoader) Retrieve() interface{} {
	for loaded.hooks.Len() > 0 {
		hook := loaded.hooks.Dequeue().(Hook)
		hook.run(loaded.target)
	}
	return loaded.target
}

// ConfigFileHook will load data from a JSON file.
type ConfigFileHook struct {
	file string
}

// CreateFileHook passing JSON file.
func CreateFileHook(file string) ConfigFileHook {
	return ConfigFileHook{file: file}
}

func (hook ConfigFileHook) run(target interface{}) {
	file, err := os.OpenFile(hook.file, os.O_RDONLY, os.ModePerm)
	if err != nil {
		log.Fatalln("Error while reading config file: ", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	err = decoder.Decode(target)
	if err != nil {
		log.Fatalln("Error while decoding config file", err)
	}
}

// ParamsHook will load data from command line params.
type ParamsHook struct {
	flags []*string
}

// CreateParamsHook creates a hook which loads
// command line params.
func CreateParamsHook() ParamsHook {
	return ParamsHook{
		flags: make([]*string, 0),
	}
}

func (hook ParamsHook) run(target interface{}) {
	hook.readFlagsFromStructMetadata(target)
	flag.Parse()
	i := 0
	foreachField(target, func(field currentField) {
		if i < len(hook.flags) && len(*hook.flags[i]) > 0 {
			setField(field.value, *hook.flags[i])
		}
		i++
	})
}

func (hook *ParamsHook) readFlagsFromStructMetadata(target interface{}) {
	foreachField(target, func(field currentField) {
		hook.flags = append(hook.flags, flag.String(field.name, "", field.name))
	})
}

// EnvHook loads data from env vars
type EnvHook struct{}

// CreateEnvHook creates a hook which loads data from
// env vars.
func CreateEnvHook() EnvHook {
	return EnvHook{}
}

func (hook EnvHook) run(target interface{}) {
	foreachField(target, func(field currentField) {
		env := os.Getenv(hook.formatEnvVar(field.name))
		if len(env) > 0 {
			setField(field.value, env)
		}
	})
}

func (hook *EnvHook) formatEnvVar(name string) string {
	upperName := strings.ToUpper(name)
	return fmt.Sprintf("CONFIG_%s", upperName)
}

func setField(field reflect.Value, rawValue string) {
	const (
		bitSize int = 64
		base    int = 10
	)
	switch field.Type().Name() {
	default:
		field.SetString(rawValue)
	case "int", "int16", "int32", "int64":
		i, err := strconv.Atoi(rawValue)
		if err != nil {
			log.Fatalln(err)
		}
		field.SetInt(int64(i))
	case "float", "float64":
		i, err := strconv.ParseFloat(rawValue, bitSize)
		if err != nil {
			log.Fatalln(err)
		}
		field.SetFloat(i)
	case "bool":
		i, err := strconv.ParseBool(rawValue)
		if err != nil {
			log.Fatalln(err)
		}
		field.SetBool(i)
	case "uint", "uint16", "uint32", "uint64":
		i, err := strconv.ParseUint(rawValue, base, bitSize)
		if err != nil {
			log.Fatalln(err)
		}
		field.SetUint(i)
	}
}

type target_t struct {
	value  reflect.Value
	typ    reflect.Type
	prefix string
}

func foreachField(target interface{}, runAction func(currentField)) {
	foreachFieldValue(target_t{
		value:  reflect.ValueOf(target).Elem(),
		typ:    reflect.TypeOf(target).Elem(),
		prefix: "",
	}, runAction)
}

func foreachFieldValue(target target_t, runAction func(currentField)) {
	for i := 0; i < target.value.NumField(); i++ {
		currentValue := target.value.Field(i)
		currentType := target.typ.Field(i)
		if currentType.Type.Kind() == reflect.Struct {
			foreachFieldValue(target_t{
				value:  currentValue,
				typ:    currentType.Type,
				prefix: currentType.Tag.Get("configPrefix"),
			}, runAction)
		} else if currentValue.IsValid() && currentValue.CanAddr() && currentValue.CanSet() {
			currentName := getFieldName(currentType)
			runAction(currentField{
				original: currentType,
				value:    currentValue,
				name:     fmt.Sprintf("%s%s", target.prefix, currentName),
				index:    i,
			})
		}
	}
}

func getFieldName(field reflect.StructField) string {
	currentTag := field.Tag.Get("configName")
	if len(currentTag) > 0 {
		return currentTag
	}
	return field.Name
}
