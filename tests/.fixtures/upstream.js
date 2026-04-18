const http = require("http");
let counts = {};
http.createServer((req, res) => {
  const p = req.url.split("?")[0];
  if (p === "/request-counts") {
    res.writeHead(200, {"Content-Type":"application/json"});
    res.end(JSON.stringify(counts));
    return;
  }
  if (p === "/reset-counts") {
    counts = {};
    res.writeHead(200);
    res.end("{}");
    return;
  }
  counts[p] = (counts[p] || 0) + 1;
  if (p.startsWith("/status/")) {
    const c = parseInt(p.split("/")[2], 10);
    res.writeHead(c, {"Content-Type":"application/json"});
    res.end(JSON.stringify({status: c}));
    return;
  }
  if (p === "/slow") {
    setTimeout(() => {
      res.writeHead(200, {"Content-Type":"application/json"});
      res.end(JSON.stringify({slow:true}));
    }, 3000);
    return;
  }
  let body = "";
  req.on("data", c => body += c);
  req.on("end", () => {
    res.writeHead(200, {"Content-Type":"application/json"});
    res.end(JSON.stringify({
      method: req.method,
      path: req.url,
      headers: req.headers,
      body: body || null,
      requestCount: counts[p]
    }));
  });
}).listen(3000, "0.0.0.0");
