# configloader
Load configuration from command line, file or env.

## How to use
Create your configuration struct, for example:
```go
type MyConfig struct {
	ListenURL string `paramName:"url"`
	Database string `paramName:"db"`
}
```

Now, you want this struct to be filled with data from a file, env or command line params. To do that,
you need to create a ConfigLoader, add hooks and retrieve your struct. Hooks are sources of data
to fill your struct. The order is very important: the later you add a hook, the higher priority it will
have. For example, you want to read configuration from file, and overwrite it if a command line param or
env variable is setted:

```go

func main() {
	config := *configloader.NewConfigLoaderFor(&MyConfig{}).
		AddHook(configloader.CreateFileHook("./config.json")).
		AddHook(configloader.CreateParamsHook()).
		AddHook(configloader.CreateEnvHook()).
		Retrieve().(*MyConfig)
	fmt.Println(config)
}
```
You can change hooks order or eliminate the ones you want. Notice that:

* env hook expects variables to be named starting with CONFIG_ followed by paramName argument or field name in uppercase. For example, for ListenURL field, its env variable will be ```CONFIG_URL```. If you delete paramName attribute, will be ```CONFIG_LISTENURL```.
* params hook expects params to be named like its paramName or field name. So ListenURL field, its parameter will be ```-url```. If you delete paramName attribute, will be ```-ListenURL```

Having in config.json this

```json
{
    "ListenURL": "localhost:8080",
    "Database": "blabla db connection",
}
```

And running this example with `CONFIG_URL=localhost:9000 go run ./main.go -db mysql`
Will return this struct `{localhost:9000 mysql}`
