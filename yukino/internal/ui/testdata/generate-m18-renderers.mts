import {homedir} from "node:os";
import {resolve} from "node:path";
import {pathToFileURL,fileURLToPath} from "node:url";
import {createHash} from "node:crypto";
import {execFileSync} from "node:child_process";
const reference=resolve(process.env.YUKINO_REFERENCE ?? resolve(homedir(),"github/yukino-code/apps/yukino"));
const fromReference=(path:string)=>import(pathToFileURL(resolve(reference,path)).href);
import fs from "node:fs";
import {createRequire} from "node:module";
const {renderMarkdown, renderStreamingMarkdown}=await fromReference("src/ui/markdown.ts");
const {formatToolOutputPreview}=await fromReference("src/ui/tool-preview.ts");
const {fitTableToWidth,renderTable}=await fromReference("src/ui/table-layout.ts");
const {wrapToLines,wrapWordsToLines}=await fromReference("src/ui/terminal-text.ts");
const {stableStreamingPrefix}=await fromReference("src/ui/streaming-markdown.ts");
const strip=(x:string)=>x.replace(/\x1b\[[0-9;]*m/g,"");
const samples=[
"hello world",
"# Heading\n\n## Second\n\n### Third\n\n#### Fourth",
"one **bold** and *italic* and ~~deleted~~ and \u0060code\u0060.",
"First line\nsecond line\n\nnext paragraph.",
"> A quote that wraps over several words and 中文.\n>\n> next line",
"- First item wraps with more words\n- Second\n  - Nested",
"3. Third\n4. Fourth",
"1) first\n2) second",
"- [x] Done\n- [ ] Pending",
"~~~unknown\none\ttwo\n中文👩‍💻\n~~~",
"\u0060\u0060\u0060js\nconst value = 1;\n\u0060\u0060\u0060",
"before\n\n---\n\nafter",
"| Name | Value |\n| --- | --- |\n| 中文 | longer words |\n| emoji | 👩‍💻 |",
"[label](https://example.com/a+b) and foo@example.com",
"git clone git@github.com:owner/repo.git",
"escape \\*stars\\* and &amp; &lt;tag&gt;",
"![alt](https://example.com/image.png \"title\")",
"one  \ntwo",
];
const markdown=samples.flatMap(text=>[1,2,5,12,24,80].flatMap(width=>["assistant","user","thinking"].map(kind=>({text,width,kind,expected:strip(renderMarkdown(text,width,kind as any))}))));
const previews=["Bash","PowerShell","Grep","Glob","ListTasks","ReadFile","AskUserQuestion","EditFile"].flatMap(name=>[1,8,24,80].map(width=>({name,width,text:Array.from({length:25},(_,i)=>i%3===0?"+ line "+i:i%3===1?"- line "+i:"  line "+i).join("\n"),expected:""}))).map(x=>({...x,expected:strip(formatToolOutputPreview(x.name,x.text,x.width))}));
const tables=[[],[["a","b"],["中文","long words"]],[["👩‍💻","x"],["value","tab\tstop"]],[["heading"],["very long value"]]].flatMap(rows=>[1,5,10,16,40,80].map(width=>{const columns=Math.max(0,...rows.map(x=>x.length));const fitted=fitTableToWidth(rows,columns,width);return{rows,width,columns,fitted,expected:fitted?strip(renderTable(fitted.rows,fitted.columnWidths)):null}}));
const wraps=["one two three four","中文abc👩‍💻def","a\tb\tc","   leading  and   trailing  ","aa\n\nbb","x".repeat(20)].flatMap(text=>[1,2,5,8,20].map(width=>({text,width,hard:wrapToLines(text,width),words:wrapWordsToLines(text,width)})));
const prefix="Introduction. ".repeat(180)+"\n\n";
const stable=[prefix+"1. first\n\n2. second\n\n",prefix+"1. first\n\n2. second\n\n3. third",prefix+"Final",prefix+"# Heading\n\nFinal","An unfinished sentence 中文 ".repeat(150),"| Name | Value |\n| --- | --- |\n"+"| key | value |\n".repeat(200),"See [the guide][guide]. ".repeat(150)+"\n\nNext"].map(text=>({text,expected:stableStreamingPrefix(text)}));
const require=createRequire(resolve(reference,"package.json"));
const syntaxSource=fs.readFileSync(resolve(reference,"src/ui/syntax-highlight.ts"),"utf8");
const syntaxNames=new Set(["plaintext","text","txt"]);
for(const match of syntaxSource.matchAll(/import \w+ from "(shiki\/langs\/[^"]+)";/g)){
 const {default:grammars}=await import(pathToFileURL(require.resolve(match[1])).href);
 for(const grammar of grammars)for(const name of [grammar.name,...(grammar.aliases??[])])syntaxNames.add(name.toLowerCase());
}
fs.writeFileSync(fileURLToPath(new URL("m18-syntax-languages.json",import.meta.url)),JSON.stringify([...syntaxNames].sort(),null,2)+"\n");
fs.writeFileSync(fileURLToPath(new URL("m18-reference.json",import.meta.url)),JSON.stringify({reference:referenceMetadata(),markdown,previews,tables,wraps,stable},null,2)+"\n");

function referenceMetadata(){
 return {commit:execFileSync("git",["-C",reference,"rev-parse","HEAD"],{encoding:"utf8"}).trim(),files:Object.fromEntries(["src/ui/marked-terminal.ts","src/ui/syntax-highlight.ts","src/theme/syntax.ts","src/ui/markdown.ts", "src/ui/tool-preview.ts", "src/ui/table-layout.ts", "src/ui/terminal-text.ts", "src/ui/streaming-markdown.ts"].map(path=>[path,createHash("sha256").update(fs.readFileSync(resolve(reference,path))).digest("hex")]))};
}
