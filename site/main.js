// Two jobs only: stagger the hero entrance, and copy the install command.
// Anything beyond that would be motion for its own sake.

(function () {
  "use strict";

  /* ---------- Entrance: hero reveals in sequence, not all at once ---------- */

  var reveals = document.querySelectorAll(".reveal");
  var reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  if (reduced) {
    // Respect the preference: show the resting state, skip the transition.
    for (var i = 0; i < reveals.length; i++) {
      reveals[i].classList.add("in");
    }
  } else {
    window.requestAnimationFrame(function () {
      for (var j = 0; j < reveals.length; j++) {
        reveals[j].classList.add("in");
      }
    });
  }

  /* ---------- Copy the install command ---------- */

  var live = document.getElementById("live");

  function announce(message) {
    if (live) {
      // Clear first so a repeated copy still announces.
      live.textContent = "";
      window.setTimeout(function () {
        live.textContent = message;
      }, 60);
    }
  }

  function label(button, text) {
    button.textContent = text;
  }

  document.addEventListener("click", function (event) {
    var button = event.target.closest(".copy");
    if (!button) {
      return;
    }

    var target = document.querySelector(button.getAttribute("data-copy"));
    if (!target) {
      return;
    }

    // textContent includes the "$ " prompt drawn for the reader; copy the
    // command itself, which is what someone actually wants to paste.
    var text = target.textContent.replace(/^\s*\$\s*/, "").trim();
    var original = button.getAttribute("data-label") || "Copy";

    function done() {
      label(button, "Copied");
      button.setAttribute("data-copied", "true");
      announce("Install command copied to the clipboard.");
      window.setTimeout(function () {
        label(button, original);
        button.removeAttribute("data-copied");
      }, 1800);
    }

    function failed() {
      // No clipboard access (insecure origin, or permission denied). The
      // command is still selectable text, so say so rather than doing nothing.
      label(button, "Select it");
      announce("Copying is unavailable here. Select the command and copy it manually.");
      window.setTimeout(function () {
        label(button, original);
      }, 1800);
    }

    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(done, failed);
    } else {
      failed();
    }
  });
})();
