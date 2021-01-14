# goink
Golang Incremental Testing Library


This package can be run at any go program root, and only tests incremental changes from a previous git commit. 

### How to run 
Use the command go get this library and install it on your GOPATH. 
```
go get github.com/braincorp/goink
```
you should now be able to run the `--help` command. By defualt, the command will be installed on your gopath. 
```
goink --help
```

### How to use 
Goink uses watches changes using git, and walks the dependency tree of your go program, finding the packages that have changed. 
It then only tests the changed packages and their dependencies using `go test`. It works essentiall as below
```
find changed files -> find changed packages -> check main packages for dependency 
```

go Ink it! 
