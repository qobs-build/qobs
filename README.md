# Quite OK Build System

Qobs is a C/C++ build system, package manager and project file generator.

Qobs's build configuration format uses [TOML](https://toml.io/), which is easy to read and might look familiar to those who used [Cargo](https://github.com/rust-lang/cargo):

```toml
[package]
name = "hello-world"
description = "Hi Qobs!"
authors = ["AzureDiamond"]

[target]
sources = ["src/**.cpp", "src/**.cc", "src/**.c"]
defines = { GRAPHICS = "1" }

[target.'target_os == "windows"']
defines = { WINDOWS = "1" }
links = ["opengl32"] # link with OpenGL on windows

[target.'target_os == "linux"']
defines = { LINUX = "1" }
links = ["m"] # link with math library

# if you are on Windows, GRAPHICS and WINDOWS will be defined

[profile.debug]
opt-level = 1 # enable some optimizations even in debug

# these will get fetched, built, linked, and included automatically:
[dependencies]
libhelloworld = "gh:zeozeozeo/libhelloworld"
# you can now #include <helloworld.h> in your program
```

([see more examples](/_examples/))

The CLI is intuitive:

```console
$ qobs new hello-world
Created file: hello-world/Qobs.toml
Created file: hello-world/src/main.c
Created file: hello-world/.gitignore

$ cd hello-world
$ qobs build .  # or just "qobs ."
...
$ qobs run .
qobs: no work to do.
Hello, World!
```

It currently supports the following build systems:

- Its own. Qobs can build the code in parallel itself, without any project generator. It has built-in support for incremental compilation.
- [Ninja](https://ninja-build.org/), use with `-g ninja`

(Visual Studio is planned)
