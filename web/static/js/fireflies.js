// Drifting embers — a handful of warm sparks that rise and fade in the upper
// right of the page. Quiet by design: honors prefers-reduced-motion by holding
// the embers still at a low glow instead of animating.
(function () {
  var canvas = document.getElementById("firefly-canvas");
  if (!canvas) return;
  var ctx = canvas.getContext("2d");

  var reduceMotion =
    window.matchMedia &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  function resize() {
    canvas.width = window.innerWidth;
    canvas.height = window.innerHeight;
  }
  resize();
  window.addEventListener("resize", resize);

  // Embers drift within this normalized zone (upper-right quadrant).
  var ZONE = { xMin: 0.44, xMax: 0.96, yMin: 0.02, yMax: 0.50 };

  var embers = [
    { x: 0.56, y: 0.17, pulseOffset: 0.0, speed: 0.00007, angle: -1.3, turnRate: 0.004, hot: 1.0 },
    { x: 0.73, y: 0.27, pulseOffset: 3.8, speed: 0.00005, angle: -1.9, turnRate: 0.003, hot: 0.82 },
    { x: 0.63, y: 0.11, pulseOffset: 7.1, speed: 0.00009, angle: -1.0, turnRate: 0.005, hot: 0.9 },
    { x: 0.85, y: 0.33, pulseOffset: 5.2, speed: 0.00004, angle: -2.2, turnRate: 0.003, hot: 0.7 },
  ];

  var lastTime = null;

  // Warm ember gradient stops scaled by alpha and per-ember heat.
  function drawEmber(ember, alpha) {
    var W = canvas.width, H = canvas.height;
    var px = ember.x * W, py = ember.y * H;
    var hot = ember.hot;

    var glowR = 2.5 + alpha * 7 * hot;

    var grad = ctx.createRadialGradient(px, py, 0, px, py, glowR * 3);
    grad.addColorStop(0,   "rgba(245, 180, 110, " + (alpha * 0.6) + ")");
    grad.addColorStop(0.4, "rgba(219, 123, 52, "  + (alpha * 0.26) + ")");
    grad.addColorStop(1,   "rgba(160, 74, 28, 0)");

    ctx.beginPath();
    ctx.arc(px, py, glowR * 3, 0, Math.PI * 2);
    ctx.fillStyle = grad;
    ctx.fill();

    ctx.beginPath();
    ctx.arc(px, py, glowR * 0.32, 0, Math.PI * 2);
    ctx.fillStyle = "rgba(252, 224, 178, " + alpha + ")";
    ctx.fill();
  }

  function flicker(ember, now) {
    var raw = Math.sin(now * 0.0007 + ember.pulseOffset);
    return Math.pow(Math.max(0, raw), 3) * 0.85;
  }

  function moveEmber(ember, dt) {
    // Bias the drift upward so embers feel like they're rising.
    ember.angle += (Math.random() - 0.5) * ember.turnRate;

    var margin = 0.03;
    if (ember.x < ZONE.xMin + margin) ember.angle += 0.05;
    if (ember.x > ZONE.xMax - margin) ember.angle -= 0.05;
    if (ember.y < ZONE.yMin + margin) ember.angle += 0.05;
    if (ember.y > ZONE.yMax - margin) ember.angle -= 0.05;

    ember.x += Math.cos(ember.angle) * ember.speed * dt;
    ember.y += Math.sin(ember.angle) * ember.speed * dt;

    ember.x = Math.max(ZONE.xMin, Math.min(ZONE.xMax, ember.x));
    ember.y = Math.max(ZONE.yMin, Math.min(ZONE.yMax, ember.y));
  }

  if (reduceMotion) {
    // Static: render each ember once at a calm, fixed glow.
    embers.forEach(function (ember) { drawEmber(ember, 0.35 * ember.hot); });
    return;
  }

  function frame(now) {
    if (!lastTime) lastTime = now;
    var dt = Math.min(now - lastTime, 50);
    lastTime = now;

    ctx.clearRect(0, 0, canvas.width, canvas.height);
    embers.forEach(function (ember) {
      moveEmber(ember, dt);
      var alpha = flicker(ember, now);
      if (alpha < 0.01) return;
      drawEmber(ember, alpha);
    });
    requestAnimationFrame(frame);
  }
  requestAnimationFrame(frame);
})();
