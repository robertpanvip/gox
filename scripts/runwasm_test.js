// 在 node 中运行 GOOS=js GOARCH=wasm 编译的 go test 二进制：
//   GOOS=js GOARCH=wasm go test -c -o /tmp/loop_wasm.test ./runtime/loop/
//   node scripts/runwasm_test.js /tmp/loop_wasm.test -test.v -test.timeout 240s
// wasm_exec.js 路径可用环境变量 GX_WASM_EXEC 覆盖（默认取 go env GOROOT）。
const fs = require("fs");
const path = require("path");
const { execSync } = require("child_process");

let wasmExec = process.env.GX_WASM_EXEC;
if (!wasmExec) {
  const goroot = execSync("go env GOROOT").toString().trim();
  wasmExec = path.join(goroot, "lib", "wasm", "wasm_exec.js");
}
require(wasmExec);

const go = new Go();
const args = process.argv.slice(2);
go.argv = [args[0] || "test.wasm", ...args.slice(1)];

const buf = fs.readFileSync(args[0]);
WebAssembly.instantiate(new Uint8Array(buf), go.importObject)
  .then((result) => go.run(result.instance))
  .catch((err) => {
    console.error("wasm load failed:", err);
    process.exit(1);
  });
