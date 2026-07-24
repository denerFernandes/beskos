import puppeteer from "puppeteer-core";
const B="http://localhost:5173";
const b=await puppeteer.launch({executablePath:"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",headless:"new",args:["--no-sandbox"]});
const S=ms=>new Promise(r=>setTimeout(r,ms));
const p=await b.newPage(); await p.setViewport({width:1400,height:900,deviceScaleFactor:1.5});
const errs=[]; p.on("pageerror",e=>errs.push("PAGEERROR "+String(e).slice(0,120)));
await p.goto(B+"/mail",{waitUntil:"networkidle2"});
if(await p.$('input[type="password"]')){ await p.type('input[type="email"]',"ana@acme-dev.com"); await p.type('input[type="password"]',"correct-horse-battery-9273"); await p.click("form.login button.send"); await S(3000); }
await p.goto(B+"/mail/mail",{waitUntil:"networkidle2"}); await S(2200);
const info=async()=>({pasta:await p.evaluate(()=>document.querySelector(".lhead h2")?.innerText), linhas:await p.evaluate(()=>document.querySelectorAll(".row").length)});
console.log("caixa:", JSON.stringify(await info()));
// estrelar a 1ª mensagem
const r=await p.evaluate(()=>{const row=document.querySelector(".row"); const s=row.querySelector(".star"); const b=s.getBoundingClientRect();
  return {assunto:row.querySelector(".s")?.innerText, x:b.x+b.width/2, y:b.y+b.height/2};});
await p.mouse.click(r.x,r.y); await S(1800);
console.log("estrelei:", JSON.stringify(r.assunto), "| marcada agora:", await p.evaluate(()=>!!document.querySelector(".row .star.on")));
// a mensagem CONTINUA na caixa?
console.log("continua na caixa:", JSON.stringify(await info()));
// aparece em Importantes?
await p.evaluate(()=>document.querySelector(".star-entry")?.click()); await S(1800);
console.log("importantes:", JSON.stringify(await info()), "| URL:", p.url().replace(B,""));
console.log("erros:", errs.length?errs.join("\n"):"nenhum");
await b.close();
