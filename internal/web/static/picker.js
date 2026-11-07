// picker.js — resolves a typed person name into the matching person id.
function resolveOption(value) {
  const list = document.getElementById('people-datalist');
  if (!list) return null;
  for (const opt of list.options) {
    if (opt.value === value) return opt;
  }
  return null;
}

document.addEventListener('submit', (event) => {
  const form = event.target;
  const inputs = form.querySelectorAll('[data-picker]');
  if (!inputs.length) return;

  let resolvedId = '';
  inputs.forEach((input) => {
    const fieldName = input.dataset.picker;
    const idField = form.querySelector(`[name="${fieldName}"]`);
    const value = input.value.trim();
    if (!value || !idField) return;
    const opt = resolveOption(value);
    if (opt && opt.dataset.id) {
      idField.value = opt.dataset.id;
      if (!resolvedId) resolvedId = opt.dataset.id;
    }
  });

  if (form.dataset.navigate === 'profile') {
    if (resolvedId) {
      event.preventDefault();
      location.href = '/person/' + resolvedId;
    }
  }
});
