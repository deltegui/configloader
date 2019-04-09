package configloader

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"reflect"

	"github.com/golang-collections/collections/queue"
)

type Hook interface {
	run(interface{})
}

type ConfigLoader struct {
	hooksStack *queue.Queue
	target     interface{}
}

func NewConfigLoaderFor(target interface{}) *ConfigLoader {
	return &ConfigLoader{
		hooksStack: queue.New(),
		target:     target,
	}
}

func (self *ConfigLoader) AddHook(hook Hook) *ConfigLoader {
	self.hooksStack.Enqueue(hook)
	return self
}

func (self ConfigLoader) Retrieve() interface{} {
	for self.hooksStack.Len() > 0 {
		hook := self.hooksStack.Dequeue().(Hook)
		hook.run(self.target)
	}
	return self.target
}

type ConfigFileHook struct {
	file string
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
	targetValue := reflect.ValueOf(target).Elem()
	for i := 0; i < targetValue.NumField(); i++ {
		currentField := targetValue.Field(i)
		if currentField.IsValid() && currentField.CanAddr() && currentField.CanSet() {
			if len(*self.flags[i]) > 0 {
				currentField.SetString(*self.flags[i])
			}
		}
	}
}

func (self *ParamsHook) readFlagsFromStructMetadata(target interface{}) {
	targetType := reflect.TypeOf(target).Elem()
	for i := 0; i < targetType.NumField(); i++ {
		currentField := targetType.Field(i)
		currentTag := currentField.Tag.Get("paramName")
		if len(currentTag) > 0 {
			self.flags = append(self.flags, flag.String(currentTag, "", currentTag))
		} else {
			self.flags = append(self.flags, flag.String(currentField.Name, "", currentField.Name))
		}
	}
}
