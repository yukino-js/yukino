import fs from "node:fs";
import { homedir } from "node:os";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { createHash } from "node:crypto";

const reference = resolve(process.env.YUKINO_REFERENCE ?? resolve(homedir(), "github/yukino-code/apps/yukino"));
const load = (path: string) => import(pathToFileURL(resolve(reference, path)).href);
const snippets = await load("src/tools/snippets.ts");
const { ComputerUseTool } = await load("src/tools/computer-use.ts");
const { runExitCleanups } = await load("src/bootstrap/exit-cleanup.ts");
const assets = new URL("../computer_use_assets/", import.meta.url);
fs.mkdirSync(assets, {recursive: true});
for (const [name, file] of Object.entries({MACOS_SNIPPET:"main.swift", WINDOWS_PWSH_SNIPPET:"screenshot.ps1", WINDOWS_PWSH_COMPILE_CSHARP_SNIPPET:"compile.ps1", WINDOWS_PWSH_ACTION_SNIPPET:"action.ps1"})) {
  fs.writeFileSync(new URL(file, assets), snippets[name]);
}
fs.writeFileSync(new URL("schema.json", assets), JSON.stringify(new ComputerUseTool({platform:"linux"}).schema().input_schema, null, 2)+"\n");
const cases = [];
const inputs = [
  {action:"left_click", coordinate:[10,20]}, {action:"click",x:0,y:0,button:"back"},
  {action:"right_click",x:10,y:20,keys:["CTRL"]}, {action:"double_click",coordinate:[0,1]},
  {action:"triple_click",coordinate:[10,20]}, {action:"mouse_move",coordinate:[5,9]},
  {action:"key",text:"CTRL+S"}, {action:"keypress",keys:["ENTER"]}, {action:"type",text:"hello 中文"},
  {action:"cursor_position"}, {action:"left_mouse_down"}, {action:"left_mouse_up"},
  {action:"left_click_drag",start_coordinate:[0,1],coordinate:[4,8]}, {action:"drag",path:[{x:1,y:2},{x:3,y:4}]},
  ...["up","down","left","right"].map(scroll_direction=>({action:"scroll",scroll_amount:3,scroll_direction})),
  {action:"scroll",scroll_x:-120,scroll_y:300,coordinate:[0,0]}, {action:"scroll",scroll_y:0},
  {action:"hold_key",keys:["CTRL"],duration:0}, {action:"wait",duration:0},
  {action:"left_click"}, {action:"type"}, {action:"key"}, {action:"zoom",region:[0,0,0,0]},
  {}, {action:"wait", actions:[{type:"wait"}]},
];
for (const input of inputs) {
  const calls: unknown[] = [];
  const tool = new ComputerUseTool({platform:"linux",runCommand:async (command:string,args:string[])=>{
    calls.push({command,args});return {code:0,stdout:Buffer.from(args[0]==="getmouselocation"?"X=40\nY=60\n":""),stderr:""};
  }});
  const result = await tool.execute({cwd: process.cwd()},input);
  cases.push({input,calls,result});
}
runExitCleanups();
fs.writeFileSync(new URL("computer-reference.json", import.meta.url), JSON.stringify({sha256:Object.fromEntries(["computer-use.ts","snippets.ts","constants.ts","prompts.ts"].map(file=>[file,createHash("sha256").update(fs.readFileSync(resolve(reference,"src/tools",file))).digest("hex")])), cases},null,2)+"\n");
