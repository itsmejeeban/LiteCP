(() => {
  "use strict";
  const queue = [];
  let active = false, sequence = 0;
  function defaults(message, input) {
    const rules = input ? [
      [/^New folder/i, "New folder", "Create folder", "Folder name"],
      [/^New file/i, "New file", "Create file", "File name"],
      [/^Archive name/i, "Create ZIP archive", "Create archive", "Archive name"],
      [/^New name/i, "Rename item", "Rename", "New name"],
      [/^Octal permissions/i, "Change permissions", "Save permissions", "Permissions"],
      [/^Destination folder/i, "Choose destination", "Continue", "Destination folder"],
      [/^Restore path/i, "Restore from Trash", "Restore", "Restore path"]
    ] : [
      [/^(Permanently (delete|empty)|Delete)/i, "Delete permanently", "Delete permanently", true],
      [/^Detach/i, "Detach website", "Detach website", true],
      [/^Remove/i, "Remove item", "Remove", true],
      [/^Discard/i, "Discard unsaved changes?", "Discard changes", true],
      [/^Move .*Trash/i, "Move to Trash", "Move to Trash", true],
      [/^Restart/i, "Restart services", "Restart services", false],
      [/^Extract/i, "Extract archive", "Extract", true],
      [/^Restore/i, "Restore backup", "Restore", true],
      [/^Import/i, "Import website", "Import", true],
      [/^Trust the SSH/i, "Verify source server identity", "Trust and connect", false],
      [/^Install/i, "Install PHP extension", "Install", false],
      [/^Enable/i, "Enable feature", "Enable", false],
      [/^Disable/i, "Disable feature", "Disable", true],
      [/^Apply/i, "Apply settings", "Apply", false],
      [/^Save/i, "Save settings", "Save", false],
      [/^Keep/i, "Activity retention", "Keep everything", false],
      [/^start /i, "Start application", "Start", false],
      [/^stop /i, "Stop application", "Stop", true]
    ];
    const rule = rules.find(item => item[0].test(message));
    return { title: rule?.[1] || (input ? "Enter details" : "Confirm action"), confirmText: rule?.[2] || "Continue", ...(input ? { label: rule?.[3] || "Value" } : { danger: rule?.[3] || false }) };
  }
  function ask(message, options, input) {
    return new Promise(resolve => {
      queue.push({ message: String(message), options, input, resolve });
      next();
    });
  }
  function next() {
    if (active || !queue.length) return;
    active = true;
    const item = queue.shift(), options = { ...defaults(item.message, item.input), ...item.options };
    options.danger = options.danger || /^(remove|delete|detach|empty)\b/i.test(options.confirmText || "");
    const previous = document.activeElement, id = `litecp-dialog-${++sequence}`;
    const dialog = document.createElement("dialog");
    dialog.className = "ui-dialog" + (options.danger ? " ui-dialog-danger" : "");
    dialog.setAttribute("aria-labelledby", `${id}-title`);
    dialog.setAttribute("aria-describedby", `${id}-description`);
    dialog.innerHTML = '<form novalidate><header><h2 data-dialog-title></h2><button class="icon-button" type="button" data-dialog-close aria-label="Close dialog"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 6 12 12M18 6 6 18"/></svg></button></header><div class="ui-dialog-body"><p data-dialog-description></p><label class="field" data-dialog-field><span data-dialog-label></span><input data-dialog-input autocomplete="off"><small data-dialog-hint></small></label><p class="ui-dialog-error" data-dialog-error role="alert" hidden></p></div><footer><button type="button" class="button button-secondary" data-dialog-cancel>Cancel</button><button type="submit" class="button button-primary" data-dialog-submit></button></footer></form>';
    const find = selector => dialog.querySelector(selector);
    find('[data-dialog-title]').id = `${id}-title`;
    find('[data-dialog-title]').textContent = options.title;
    find('[data-dialog-description]').id = `${id}-description`;
    find('[data-dialog-description]').textContent = item.message;
    find('[data-dialog-submit]').textContent = options.confirmText;
    const field = find('[data-dialog-field]'), control = find('[data-dialog-input]');
    field.hidden = !item.input;
    if (item.input) {
      find('[data-dialog-label]').textContent = options.label;
      control.type = options.type || "text";
      control.value = options.value ?? "";
      control.required = options.required ?? false;
      if (options.maxLength) control.maxLength = options.maxLength;
      if (options.placeholder) control.placeholder = options.placeholder;
      find('[data-dialog-hint]').textContent = options.hint || "";
      find('[data-dialog-hint]').hidden = !options.hint;
      control.addEventListener('input', () => { control.setCustomValidity('');control.removeAttribute('aria-invalid');find('[data-dialog-error]').hidden=true; });
    }
    let finished = false;
    function finish(value) {
      if (finished) return;
      finished = true;
      if (control.type === "password") control.value = "";
      window.removeEventListener('hashchange', cancel);
      dialog.close(); dialog.remove();
      if (previous?.isConnected && !previous.disabled) previous.focus({ preventScroll: true });
      active = false;
      item.resolve(value);
      next();
    }
    const cancel = () => finish(item.input ? null : false);
    if (options.dismissOnNavigation) window.addEventListener('hashchange', cancel);
    find('[data-dialog-close]').addEventListener('click', cancel);
    find('[data-dialog-cancel]').addEventListener('click', cancel);
    dialog.addEventListener('cancel', event => { event.preventDefault(); event.stopPropagation(); cancel(); });
    dialog.addEventListener('keydown', event => event.stopPropagation());
    dialog.addEventListener('close', () => { if (!finished) cancel(); });
    find('form').addEventListener('submit', event => {
      event.preventDefault();
      if (item.input && options.match !== undefined && control.value !== options.match) control.setCustomValidity(`Enter ${options.match} to continue.`);
      if (!find('form').checkValidity()) { find('[data-dialog-error]').textContent=control.validationMessage;find('[data-dialog-error]').hidden=false;control.setAttribute('aria-invalid','true');control.focus();return; }
      finish(item.input ? control.value : true);
    });
    document.body.append(dialog);
    dialog.showModal();
    (item.input ? control : options.danger ? find('[data-dialog-cancel]') : find('[data-dialog-submit]')).focus();
  }
  window.LiteCPDialogs = {
    confirm: (message, options = {}) => ask(message, options, false),
    prompt: (message, value = "", options = {}) => ask(message, { value, ...options }, true)
  };
})();
