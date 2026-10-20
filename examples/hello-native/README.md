# Hello Native

Minimal BYOP P4.1 native plugin. It declares one command, `hello.say`, and
activates only when that command runs for the first time.

```sh
cd examples/hello-native
go build -o bin/hello-native .
cd ../..
BYOP_PLUGINS_DIR="$PWD/examples" BYOP_PAGE=plugins ./byop
```

Click **Say hello** on the plugin card. BYOP starts the process, sends
`initialize`, `activate` and `invokeCommand`, and shows the result. Click
**Deactivate** to send `deactivate`, close stdin and wait for the process to
exit. The compiled `bin/` folder is ignored by Git.
