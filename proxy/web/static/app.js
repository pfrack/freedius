// Global HTMX wiring + small helpers shared by every page.
//
// showToast appends a transient message to #toast-region (defined in
// layout.html). It uses textContent (never innerHTML) so user-controlled
// messages cannot inject markup or scripts.
//
// escapeHTML is the same sanitizer used inside the inline error slot in the
// htmx:afterRequest handler below.
//
// The mapping-details drawer is wired here too: openDrawer / closeDrawer
// toggle the .drawer--open class on #mapping-drawer, focus management
// moves keyboard focus into the drawer on open and returns it to the
// triggering row on close, and Escape closes an open drawer. The drawer
// container and row triggers are defined in templates/index.html.

function showToast(message, kind) {
  var region = document.getElementById('toast-region');
  if (!region) return;
  var toast = document.createElement('div');
  toast.className = 'toast' + (kind === 'success' ? ' toast--success' : '');
  toast.textContent = message;
  region.appendChild(toast);
  setTimeout(function() {
    if (toast.parentNode) toast.parentNode.removeChild(toast);
  }, 4000);
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, function(c) {
    return ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[c];
  });
}

// drawerOpener remembers the element that triggered the current drawer
// open so closeDrawer() can return focus to it.
var drawerOpener = null;

// openDrawer marks the drawer container as open and moves focus into it.
// Called automatically by the htmx:afterSwap handler below; not invoked
// directly so the swap and open are atomic from the user's perspective.
function openDrawer(drawer) {
  if (!drawer) return;
  drawer.classList.add('drawer--open');
  setTimeout(function() {
    var closeBtn = drawer.querySelector('.drawer__close');
    if (closeBtn) closeBtn.focus();
  }, 0);
}

// closeDrawer hides the drawer, clears its content, and returns focus to
// the element that opened it. Invoked by the close button (inline onclick
// in mapping-drawer.html) and the Escape key handler.
function closeDrawer() {
  var drawer = document.getElementById('mapping-drawer');
  if (drawer) {
    drawer.classList.remove('drawer--open');
    // Clear content so stale fragments don't linger for screen readers or
    // future HTMX swaps that re-target the container.
    drawer.innerHTML = '';
  }
  if (drawerOpener && drawerOpener.focus) {
    drawerOpener.focus();
  }
  drawerOpener = null;
}

// Capture the trigger before HTMX swaps the drawer content in, so we
// can restore focus to that exact element on close. Limiting to drawer
// requests leaves drawerOpener untouched for unrelated htmx:beforeRequest
// events.
document.body.addEventListener('htmx:beforeRequest', function(evt) {
  if (evt.target && evt.target.id === 'mapping-drawer') {
    drawerOpener = document.activeElement;
  }
});

document.body.addEventListener('htmx:afterSwap', function(evt) {
  if (evt.target && evt.target.id === 'mapping-drawer') {
    openDrawer(evt.target);
  }
});

// Escape closes the drawer when it's open. Document-level so the key
// works regardless of which element inside the drawer has focus.
document.addEventListener('keydown', function(evt) {
  if (evt.key !== 'Escape') return;
  var drawer = document.getElementById('mapping-drawer');
  if (drawer && drawer.classList.contains('drawer--open')) {
    closeDrawer();
  }
});

document.body.addEventListener('htmx:afterRequest', function(evt) {
  var xhr = evt.detail && evt.detail.xhr;
  if (!xhr) return;
  var successful = evt.detail.successful;
  var target = evt.detail.target || evt.target;
  var verb = (xhr.requestBody && xhr.requestBody.length > 0) ||
             (evt.detail.requestConfig && evt.detail.requestConfig.verb !== 'GET');

  // Reset any inline form-error slots when the response is successful.
  if (successful) {
    var slot = (target && target.closest && target.closest('dialog') &&
                target.closest('dialog').querySelector('.form-error')) || null;
    if (slot) slot.textContent = '';
    // Brief success toast for mutations (POST/PUT/DELETE).
    if (verb) showToast('Saved', 'success');
    return;
  }

  // 401 when no auth token is configured is impossible; with auth token,
  // surface as a toast. 4xx/5xx from form submissions populate the
  // matching inline-error slot; everything else becomes a toast.
  var status = xhr.status;
  var errMsg = '';
  var fields = null;
  try {
    var data = JSON.parse(xhr.responseText || '{}');
    errMsg = data.message || data.error || '';
    fields = data.fields || null;
  } catch (e) { /* not JSON; fall through */ }

  var dialog = target && target.closest && target.closest('dialog');
  if (dialog) {
    var slot = dialog.querySelector('.form-error');
    if (slot) {
      // Build with escapeHTML so errMsg / fields can't inject markup.
      var html = '';
      if (errMsg) html += '<div>' + escapeHTML(errMsg) + '</div>';
      if (fields) {
        html += '<ul>';
        for (var k in fields) {
          if (Object.prototype.hasOwnProperty.call(fields, k)) {
            html += '<li><strong>' + escapeHTML(k) + '</strong>: ' +
                      escapeHTML(fields[k]) + '</li>';
          }
        }
        html += '</ul>';
      }
      slot.innerHTML = html;
      // Keep the dialog open on validation failure.
      return;
    }
  }

  // Non-form request (delete, fetch, etc.): global toast.
  showToast(errMsg || ('Request failed (' + status + ')'), 'error');
});