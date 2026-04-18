const http = require("http");
const fs = require("fs");
let counts = {};

// 1x1 red PNG (67 bytes)
const pngBytes = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==",
  "base64"
);

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

  if (p === "/text") {
    res.writeHead(200, {"Content-Type":"text/plain", "X-Custom":"hello"});
    res.end("plain text response");
    return;
  }

  if (p === "/image") {
    res.writeHead(200, {"Content-Type":"image/png", "Content-Length": pngBytes.length.toString()});
    res.end(pngBytes);
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
