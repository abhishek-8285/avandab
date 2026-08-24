/* Bookings kanban drag wiring (Spec 22 §4.2).
 * Drag targets map onto the EXISTING status endpoints:
 *   pending   -> (source lane)
 *   confirmed -> POST /bookings/{id}/confirm
 *   completed -> POST /bookings/{id}/complete
 *   cancelled -> POST /bookings/{id}/cancel
 * Backwards drags are rejected client-side; the server remains the
 * authority and rejects invalid transitions regardless (edge case 9). */
(function () {
  "use strict";

  var ORDER = ["pending", "confirmed", "completed", "cancelled"];
  var TARGET = {
    confirmed: "/bookings/{id}/confirm",
    completed: "/bookings/{id}/complete",
    cancelled: "/bookings/{id}/cancel",
    // pending is the source lane: no endpoint — backwards move.
  };

  function feedback(msg, isError) {
    var box = document.getElementById("board-feedback");
    if (!box) return;
    box.textContent = msg;
    box.classList.remove("hidden");
    if (isError) { box.classList.add("text-error"); box.classList.remove("text-primary"); }
    else { box.classList.add("text-primary"); box.classList.remove("text-error"); }
  }

  function rank(status) {
    return ORDER.indexOf(status);
  }

  function init() {
    var board = document.getElementById("board");
    if (!board) return;

    var dragged = null;

    board.addEventListener("dragstart", function (ev) {
      var card = ev.target.closest ? ev.target.closest(".board-card") : null;
      if (!card) return;
      dragged = card;
      card.style.opacity = "0.5";
      try { ev.dataTransfer.setData("text/plain", card.dataset.id); } catch (e) {}
    });

    board.addEventListener("dragend", function () {
      if (dragged) { dragged.style.opacity = ""; dragged = null; }
    });

    board.querySelectorAll(".board-column").forEach(function (col) {
      col.addEventListener("dragover", function (ev) { ev.preventDefault(); });
      col.addEventListener("drop", function (ev) {
        ev.preventDefault();
        if (!dragged) return;
        var target = col.dataset.status;
        var from = dragged.dataset.status;
        var id = dragged.dataset.id;

        if (target === from) return;
        if (rank(target) < rank(from)) {
          feedback("Backwards move rejected: " + from + " → " + target, true);
          return;
        }
        var tpl = TARGET[target];
        if (!tpl) {
          feedback("No transition to " + target, true);
          return;
        }
        fetch(tpl.replace("{id}", encodeURIComponent(id)), {
          method: "POST",
          credentials: "same-origin",
        }).then(function (r) {
          if (r.ok || r.status === 302 || r.status === 303) {
            window.location.reload(); // server-rendered board stays truthful
          } else {
            feedback("Transition rejected (" + r.status + ")", true);
            dragged.style.opacity = "";
          }
        }).catch(function () {
          feedback("Network error", true);
          dragged.style.opacity = "";
        });
      });
    });

    /* Live sync: booking events arrive on the shared SSE hub (≤2s). */
    if (window.EventSource) {
      var es = new EventSource("/api/v1/telemetry/stream");
      es.addEventListener("telemetry", function (ev) {
        var msg;
        try { msg = JSON.parse(ev.data); } catch (e) { return; }
        if (msg && msg.booking_id) {
          window.location.reload();
        }
      });
    }
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
