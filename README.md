# config-loader
Load configuration from command line or file

Example:

```go
type Demo struct {
	Hello string `paramName:"hello" json:"hello"`
	Adios string `paramName:"dos" json:"dos"`
}

func main() {
	config := NewConfigLoaderFor(&Demo{}).
		AddHook(ConfigFileHook{file: "./demo.json"}).
		AddHook(CreateParamsHook()).
		Retrieve()
	fmt.Println(config)
}
```

Having in demo.json this

```json
{
    "hello": "que pasa",
    "dos": "uno"
}
```

And running this example with `go run ./main.go -hello adios`
Will return this struct `&{adios uno}`
