# apriori
Go proxy with a special care for prior packages

## Rationale
This go modules proxy is designed for building environments, where we desire to relieve a burden of numerous downloads
and reduce an amount of weak links.

It is meant to be run locally, right on the builder machine and having some especially nasty packages pre-loaded 
in a predictable manner.

## Design

This tool consists of two parts:

1. Cache generator `apriori generate`
2. Go modules proxy `apriori serve`

### How cache generator works

Cache generator recieves a list of modules to store in cache. The list should look like

```
github.com/sirkon/message@v1.5.1
github.com/rs/zerolog
go.uber.org/zap@v1.9.1
```  

where we requested to store version `v1.5.1` of `github.com/sirkon/message`, version `v1.9.1` of `go.uber.org/zap` 
and the latest version of `github.com/rs/zerolog`.

Now, the generator retrieves revision info, go.mod content and zipped source archvive for a requested version of a 
module and saves it in requested places (directory for go.mod files, directory for source archives and file to 
store apriori info in json format should be set, see `apriori generate --help` for details)

> You can download each module dependencies as well: put `-r` or `--recursive` option for `generate`.

### How server works

Server should get a path to saved apriori info file to get the stored info.

Now, when it recieves a request it looks for the module in the stored cache. 

> The situation when the cache has a requested module but lacks the requested version of the module is an error

So, requested module found the client gets stored data

In case there's no requested module in the cache the backing module fetcher (either legacy VCS or another go 
modules instance) is asked for it.

## How to use

First generate apriori cache, then use it. Remember though, once you copy apriori cache somewhere in order to use it 
there you either must copy go.mod-s and source archives to exactly same paths or to appropriately change apriori info 
path values for these files.

## Examples

##### Storing requested modules using legacy module fetcher 

```bash
apriori generate --source modules.lst --dest apriori.json --gomod-dir ./gomods --source-dirs ./sources
```

##### Serving using just generated data using `https://proxy.golang.org` as module fetcher 
```
apriori serve --listen 0.0.0.0:8181 --apriori apriori.json
```

##### Same, serving using just generated apriori data, but using company's internal go modules proxy
```
apriori --use-goproxy https://goproxy.company.com serve --listen 0.0.0.0:8181 --apriori apriori.json
```
  
Remember, module fetcher options are the same for both generation and serving
