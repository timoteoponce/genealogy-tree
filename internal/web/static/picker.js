// picker.js — resolves a typed person name into the matching person id on
// form submit, so "search by name" fields can submit a numeric id. Each
// search input carries data-picker="<field-name>"; the hidden input with
// that name receives the resolved id. No match → left empty.
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
  form.querySelectorAll('[data-picker]').forEach((input) => {
    const fieldName = input.dataset.picker;
    const idField = form.querySelector(`[name="${fieldName}"]`);
    const value = input.value.trim();
    if (!value || !idField) return;
    const opt = resolveOption(value);
    if (opt && opt.dataset.id) idField.value = opt.dataset.id;
  });
});
