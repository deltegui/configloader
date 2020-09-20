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
	value reflect.Value
	name  string
	index int
}

type Hook interface {
	run(interface{})
}

type ConfigLoader struct {
	hooks  *queue.Queue
	target interface{}
}

func NewConfigLoaderFor(target interface{}) *ConfigLoader {
	return &ConfigLoader{
		hooks:  queue.New(),
		target: target,
	}
}

func (self *ConfigLoader) AddHook(hook Hook) *ConfigLoader {
	self.hooks.Enqueue(hook)
	return self
}

func (self ConfigLoader) Retrieve() interface{} {
	for self.hooks.Len() > 0 {
		hook := self.hooks.Dequeue().(Hook)
		hook.run(self.target)
	}
	return self.target
}

type ConfigFileHook struct {
	file string
}

func CreateFileHook(file string) ConfigFileHook {
	return ConfigFileHook{file: file}
}

func (self ConfigFileHook) run(target interface{}) {
	file, err := os.OpenFile(self.file, os.O_RDONLY, os.ModePerm)
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

type ParamsHook struct {
	flags []*string
}

func CreateParamsHook() ParamsHook {
	return ParamsHook{
		flags: make([]*string, 0, 0),
	}
}

func (self ParamsHook) run(target interface{}) {
	self.readFlagsFromStructMetadata(target)
	flag.Parse()
	foreachField(target, func(field currentField) {
		if len(*self.flags[field.index]) > 0 {
			setField(field.value, *self.flags[field.index])
		}
	})
}

func (self *ParamsHook) readFlagsFromStructMetadata(target interface{}) {
	targetType := reflect.TypeOf(target).Elem()
	for i := 0; i < targetType.NumField(); i++ {
		currentField := targetType.Field(i)
		fieldName := getFieldName(currentField)
		self.flags = append(self.flags, flag.String(fieldName, "", fieldName))
	}
}

type EnvHook struct{}

func CreateEnvHook() EnvHook {
	return EnvHook{}
}

func (self EnvHook) run(target interface{}) {
	foreachField(target, func(field currentField) {
		env := os.Getenv(self.formatEnvVar(field.name))
		if len(env) > 0 {
			setField(field.value, env)
		}
	})
}

func (self *EnvHook) formatEnvVar(name string) string {
	upperName := strings.ToUpper(name)
	return fmt.Sprintf("CONFIG_%s", upperName)
}

func getFieldName(field reflect.StructField) string {
	currentTag := field.Tag.Get("paramName")
	if len(currentTag) > 0 {
		return currentTag
	}
	return field.Name
}

func setField(field reflect.Value, rawValue string) {
	const (
		bitSize int = 64
		base    int = 10
	)
	switch field.Type().Name() {
	default:
		field.SetString(rawValue)
		break
	case "int":
	case "int16":
	case "int32":
	case "int64":
		i, err := strconv.Atoi(rawValue)
		if err != nil {
			log.Fatalln(err)
		}
		field.SetInt(int64(i))
		break
	case "float":
	case "float64":
		i, err := strconv.ParseFloat(rawValue, bitSize)
		if err != nil {
			log.Fatalln(err)
		}
		field.SetFloat(i)
		break
	case "bool":
		i, err := strconv.ParseBool(rawValue)
		if err != nil {
			log.Fatalln(err)
		}
		field.SetBool(i)
		break
	case "uint":
	case "uint16":
	case "uint32":
	case "uint64":
		i, err := strconv.ParseUint(rawValue, base, bitSize)
		if err != nil {
			log.Fatalln(err)
		}
		field.SetUint(i)
		break
	}

}

func foreachField(target interface{}, runAction func(currentField)) {
	targetValue := reflect.ValueOf(target).Elem()
	targetType := reflect.TypeOf(target).Elem()
	for i := 0; i < targetValue.NumField(); i++ {
		currentValue := targetValue.Field(i)
		currentType := targetType.Field(i)
		if currentValue.IsValid() && currentValue.CanAddr() && currentValue.CanSet() {
			currentName := getFieldName(currentType)
			runAction(currentField{
				value: currentValue,
				name:  currentName,
				index: i,
			})
		}
	}
}
