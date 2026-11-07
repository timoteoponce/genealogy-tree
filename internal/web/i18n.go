// Package web i18n: a tiny translation layer.
//
// English is the source language and the fallback: keys are the English strings
// themselves, and a missing Spanish entry simply returns the English key. That
// keeps templates readable ("{{t $.Lang "People"}}" renders "People" by default)
// and guarantees the UI never shows an empty/missing string.
package web

import (
	"fmt"
	"net/http"
	"strings"
)

// langFromAccept picks the first language tag out of an Accept-Language header.
func langFromAccept(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return "en"
	}
	// First comma-separated entry; strip quality value.
	part := strings.SplitN(h, ",", 2)[0]
	part = strings.SplitN(part, ";", 2)[0]
	part = strings.TrimSpace(part)
	if l := normalizeLang(part); l != "" {
		return l
	}
	return "en"
}

// normalizeLang maps a raw language tag/code to one of the supported codes.
func normalizeLang(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	// Accept both "es" and "es-ES" / "es_MX".
	code := raw
	if i := strings.IndexAny(code, "-_"); i >= 0 {
		code = code[:i]
	}
	switch code {
	case "es":
		return "es"
	case "en":
		return "en"
	}
	return ""
}

// tr translates key into lang, falling back to English (the key itself) when
// no Spanish entry exists. args are applied via fmt.Sprintf.
func tr(lang, key string, args ...any) string {
	if lang == "" {
		lang = "en"
	}
	if lang == "es" {
		if v, ok := esTranslations[key]; ok {
			return formatTr(v, args)
		}
	}
	return formatTr(key, args)
}

// formatTr applies args to a format string. Kept non-variadic so vet does not
// treat the translation helpers as printf-like functions.
func formatTr(format string, args []any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

// (s *Server) tr is a convenience wrapper used by handlers.
func (s *Server) tr(r *http.Request, key string, args ...any) string {
	return tr(s.requestLang(r), key, args...)
}

// requestLang resolves the active language for a request.
func (s *Server) requestLang(r *http.Request) string {
	if r == nil {
		return "en"
	}
	if v := r.URL.Query().Get("lang"); v != "" {
		if l := normalizeLang(v); l != "" {
			return l
		}
	}
	if c, err := r.Cookie("lang"); err == nil {
		if l := normalizeLang(c.Value); l != "" {
			return l
		}
	}
	return langFromAccept(r.Header.Get("Accept-Language"))
}

// esTranslations holds the Spanish overrides. Keys are the English source
// strings (so missing entries fall back to English automatically).
var esTranslations = map[string]string{
	// Navigation / common
	"People":               "Personas",
	"Tree":                 "Árbol",
	"Admin":                "Administración",
	"Log in":               "Iniciar sesión",
	"Log out":              "Cerrar sesión",
	"Back home":            "Volver al inicio",
	"Search":               "Buscar",
	"Clear":                "Limpiar",
	"Save":                 "Guardar",
	"Add":                  "Añadir",
	"Delete":               "Eliminar",
	"Edit":                 "Editar",
	"Upload":               "Subir",
	"Attach":               "Adjuntar",
	"detach":               "desvincular",
	"unlink":               "desvincular",
	"Set role":             "Asignar rol",
	"Set password":         "Asignar contraseña",
	"Create user":          "Crear usuario",
	"Create family":        "Crear familia",
	"Create admin account": "Crear cuenta de administrador",
	"Save settings":        "Guardar configuración",
	"Save family":          "Guardar familia",
	"Save changes":         "Guardar cambios",

	// Header / footer
	"genealogy-tree · single-binary family archive": "genealogy-tree · archivo familiar en un solo binario",

	// Dashboard
	"Welcome to %s": "Bienvenido a %s",
	"Browse and grow your family history — everyone is here.": "Explora y amplía tu historia familiar — todos están aquí.",
	"Family tree":    "Árbol familiar",
	"people":         "personas",
	"families":       "familias",
	"photos":         "fotos",
	"sources":        "fuentes",
	"Recently added": "Añadidos recientemente",
	"No people yet — an admin can import a GEDCOM under %s.": "Aún no hay personas — un administrador puede importar un GEDCOM en %s.",

	// People list
	"People (%d)":     "Personas (%d)",
	"Search by name…": "Buscar por nombre…",
	"No matches.":     "Sin resultados.",
	"page %d / %d":    "página %d / %d",
	"← newer":         "← más recientes",
	"older →":         "más antiguas →",
	"living":          "viva",
	"edit":            "editar",

	// Person page
	"male":               "hombre",
	"female":             "mujer",
	"other":              "otro",
	"sex unknown":        "sexo desconocido",
	"Born":               "Nacimiento",
	"Died":               "Fallecimiento",
	"Life facts":         "Datos vitales",
	"Notes":              "Notas",
	"Photos (%d)":        "Fotos (%d)",
	"portrait":           "retrato",
	"placeholder":        "marcador",
	"set portrait":       "definir retrato",
	"delete":             "eliminar",
	"Upload photo":       "Subir foto",
	"caption (optional)": "leyenda (opcional)",
	"Sources":            "Fuentes",
	"(untitled source)":  "(fuente sin título)",
	"Parents":            "Padres",
	"Siblings":           "Hermanos",
	"Spouse & children":  "Cónyuge e hijos",
	"Spouse:":            "Cónyuge:",
	"unknown":            "desconocido",
	"married %s":         "casados %s",
	"+ add parents":      "+ añadir padres",
	"View tree":          "Ver árbol",
	"Add child":          "Añadir hijo/a",
	"Add spouse":         "Añadir cónyuge",
	"Add parent":         "Añadir padre/madre",
	"Add sibling":        "Añadir hermano/a",

	// Forms
	"First names":                   "Nombres",
	"Surname":                       "Apellido",
	"Married name (optional)":       "Apellido de casada (opcional)",
	"Nickname (optional)":           "Apodo (opcional)",
	"Sex":                           "Sexo",
	"Unknown":                       "Desconocido",
	"Male":                          "Hombre",
	"Female":                        "Mujer",
	"Other":                         "Otro",
	"Birth — date or year":          "Nacimiento — fecha o año",
	"Birth — place":                 "Nacimiento — lugar",
	"Death — date or year":          "Fallecimiento — fecha o año",
	"Death — place":                 "Fallecimiento — lugar",
	"leave empty for living people": "dejar vacío para personas vivas",
	"e.g. \"1847\", \"ABT 1850\", \"MAY 1901\"": "p. ej. \"1847\", \"ABT 1850\", \"MAY 1901\"",
	"New person":  "Nueva persona",
	"Edit person": "Editar persona",
	"Add person":  "Añadir persona",

	// Add child / sibling
	"Into which family?":                          "¿En qué familia?",
	"A new family will be created automatically.": "Se creará una familia nueva automáticamente.",
	"Link an existing person (search by name)":    "Vincular persona existente (buscar por nombre)",
	"…or create a new person":                     "…o crear una persona nueva",
	"Link type":                                   "Tipo de vínculo",
	"Biological":                                  "Biológico",
	"Adopted":                                     "Adoptado/a",
	"Foster":                                      "Acogida",
	"Birth (year or date)":                        "Nacimiento (año o fecha)",
	"…or create a new spouse":                     "…o crear un nuevo cónyuge",
	"Auto (opposite of %s)":                       "Auto (opuesto de %s)",

	// Family
	"Family":                  "Familia",
	"Edit family":             "Editar familia",
	"Partner 1":               "Pareja 1",
	"Partner 2":               "Pareja 2",
	"Details":                 "Detalles",
	"Status":                  "Estado",
	"Marriage — date or year": "Matrimonio — fecha o año",
	"Marriage — place":        "Matrimonio — lugar",
	"Children (%d)":           "Hijos (%d)",
	"No children linked yet.": "Aún no hay hijos vinculados.",
	"married":                 "casados",
	"divorced":                "divorciados",
	"unmarried":               "no casados",
	"separated":               "separados",
	// Tree page
	"Focused on %s":         "Centrado en %s",
	"profile":               "perfil",
	"Search for a person…":  "Buscar una persona…",
	"Open profile":          "Abrir perfil",
	"gens":                  "gens",
	"Vertical":              "Vertical",
	"Horizontal":            "Horizontal",
	"No recorded parents.":  "Sin padres registrados.",
	"No recorded children.": "Sin hijos registrados.",
	"Ancestors":             "Ancestros",
	"Descendants":           "Descendientes",
	"Showing %d people in %s view — use the search box above to focus on one person.": "Mostrando %d personas en vista %s — usa el buscador de arriba para centrarte en una persona.",
	"Nothing to show yet — an admin can %s to populate the tree.":                     "Nada que mostrar aún — un administrador puede %s para poblar el árbol.",

	// Admin
	"users":             "usuarios",
	"Users":             "Usuarios",
	"Role updated":      "Rol actualizado",
	"Password updated":  "Contraseña actualizada",
	"Saved.":            "Guardado.",
	"Import GEDCOM":     "Importar GEDCOM",
	"Import report":     "Informe de importación",
	"Import a new tree": "Importar un árbol nuevo",
	"Settings":          "Configuración",
	"Site name (shown in the top bar and titles)": "Nombre del sitio (se muestra en la barra superior y títulos)",
	"Audit log":     "Registro de auditoría",
	"At":            "Fecha",
	"User":          "Usuario",
	"Action":        "Acción",
	"Entity":        "Entidad",
	"ID":            "ID",
	"Detail":        "Detalle",
	"system":        "sistema",
	"History":       "Historial",
	"Warnings (%d)": "Avisos (%d)",
	"No warnings.":  "Sin avisos.",
	"Media refs":    "Referencias multimedia",

	// Login / setup
	"Username":                        "Usuario",
	"Password":                        "Contraseña",
	"Welcome! Set up the first admin": "¡Bienvenido! Configura el primer administrador",
	"There are no accounts yet — create the administrator that will\n    manage users, imports and settings.": "Aún no hay cuentas — crea el administrador que gestionará\n    usuarios, importaciones y configuración.",
	"Repeat password": "Repetir contraseña",

	// Errors
	"Unknown person.":                              "Persona desconocida.",
	"That person does not exist.":                  "Esa persona no existe.",
	"That person does not exist (or was removed).": "Esa persona no existe (o fue eliminada).",
	"Unknown family.":                              "Familia desconocida.",
	"That family does not exist.":                  "Esa familia no existe.",
	"Unknown user.":                                "Usuario desconocido.",
	"Database unavailable.":                        "Base de datos no disponible.",
	"Error":                                        "Error",

	"Actions":                                "Acciones",
	"Add child of %s":                        "Añadir hijo/a de %s",
	"Add father":                             "Añadir padre",
	"Add mother":                             "Añadir madre",
	"Add parents":                            "Añadir padres",
	"Add parents of":                         "Añadir padres de",
	"Add parents of %s":                      "Añadir padres de %s",
	"Add sibling of %s":                      "Añadir hermano/a de %s",
	"Add spouse of":                          "Añadir cónyuge de",
	"Add spouse of %s":                       "Añadir cónyuge de %s",
	"Adopted child":                          "Hijo/a adoptado/a",
	"Attach a person":                        "Adjuntar una persona",
	"Attach photo":                           "Adjuntar foto",
	"Audit write failed.":                    "Error al escribir en la auditoría.",
	"Biological child":                       "Hijo/a biológico/a",
	"Cannot marry a person to themselves.":   "No se puede casar a una persona consigo misma.",
	"Chart type":                             "Tipo de gráfico",
	"Child link type":                        "Tipo de vínculo del hijo/a",
	"Could not read the upload.":             "No se pudo leer la subida.",
	"Could not save changes.":                "No se pudieron guardar los cambios.",
	"Date or year":                           "Fecha o año",
	"Delete family":                          "Eliminar familia",
	"Delete person":                          "Eliminar persona",
	"Deletion needs the confirm box ticked.": "Marca la casilla de confirmación para eliminar.",
	"Edit %s":                                "Editar %s",
	"Events":                                 "Eventos",
	"Families":                               "Familias",
	"Family events":                          "Eventos familiares",
	"Father":                                 "Padre",
	"File":                                   "Archivo",
	"Focused on":                             "Centrado en",
	"Foster child":                           "Hijo/a en acogida",
	"GEDCOM file (.ged)":                     "Archivo GEDCOM (.ged)",
	"Generations":                            "Generaciones",
	"Home":                                   "Inicio",
	"I am sure":                              "Estoy seguro/a",
	"I am sure — this permanently deletes the person": "Estoy seguro/a — esto elimina permanentemente a la persona",
	"Import (replaces tree)":                          "Importar (reemplaza el árbol)",
	"Latest imports":                                  "Últimas importaciones",
	"Leave a parent empty to skip them.":              "Deja un padre/madre vacío para omitirlo.",
	"Link an existing child":                          "Vincular un hijo/a existente",
	"Link an existing person":                         "Vincular una persona existente",
	"Marriage":                                        "Matrimonio",
	"Media":                                           "Multimedia",
	"Mother":                                          "Madre",
	"Name":                                            "Nombre",
	"No file in the upload.":                          "No hay archivo en la subida.",
	"No people yet — an admin can import a GEDCOM under":        "Aún no hay personas — un administrador puede importar un GEDCOM en",
	"Open a person's profile":                                   "Abrir el perfil de una persona",
	"Photo attached but profile marking failed.":                "Foto adjuntada pero falló el marcado de retrato.",
	"Pick an existing child or fill in the fields.":             "Elige un hijo/a existente o rellena los campos.",
	"Pick an existing person or fill in the new spouse fields.": "Elige una persona existente o rellena los campos del cónyuge.",
	"Place":                      "Lugar",
	"Recent activity":            "Actividad reciente",
	"Role":                       "Rol",
	"Started":                    "Iniciado",
	"That child does not exist.": "Ese hijo/a no existe.",
	"That family does not exist (or was removed).":                            "Esa familia no existe (o fue eliminada).",
	"That media row does not exist.":                                          "Esa fila multimedia no existe.",
	"That photo does not exist.":                                              "Esa foto no existe.",
	"The chosen person does not exist.":                                       "La persona elegida no existe.",
	"The chosen spouse does not exist.":                                       "El cónyuge elegido no existe.",
	"Their other parent is":                                                   "Su otro padre/madre es",
	"This area is restricted to administrators.":                              "Esta área está restringida a administradores.",
	"This person has no recorded parents, so a sibling cannot be added here.": "Esta persona no tiene padres registrados, por lo que no se puede añadir un hermano/a aquí.",
	"Tree: %s":     "Árbol: %s",
	"Type a name…": "Escribe un nombre…",
	"Uploading a GEDCOM replaces the whole current tree. Users, settings and import history are kept. Photos referenced by the file become placeholders — the actual images ship in a separate zip from MyHeritage and can be attached on the person pages.": "Subir un GEDCOM reemplaza todo el árbol actual. Se conservan usuarios, configuración e historial de importaciones. Las fotos referenciadas por el archivo se convierten en marcadores — las imágenes reales se envían en un zip aparte de MyHeritage y se pueden adjuntar en las páginas de persona.",
	"Welcome":                             "Bienvenido",
	"Wrong username or password.":         "Usuario o contraseña incorrectos.",
	"admin":                               "admin",
	"click a card to refocus":             "haz clic en una tarjeta para recentrar",
	"done":                                "hecho",
	"drag to pan":                         "arrastra para desplazar",
	"e.g. 1920":                           "p. ej. 1920",
	"e.g. 1994":                           "p. ej. 1994",
	"e.g. \\\"1948\\\", \\\"ABT 1949\\\"": "p. ej. \\\"1948\\\", \\\"ABT 1949\\\"",
	"e.g. \\\"2011-05-14\\\", \\\"ABT 1949\\\"": "p. ej. \\\"2011-05-14\\\", \\\"ABT 1949\\\"",
	"editor":                   "editor",
	"expected ?person=<id>":    "se esperaba ?person=<id>",
	"for":                      "para",
	"import a GEDCOM":          "importar un GEDCOM",
	"min 8 characters":         "mín. 8 caracteres",
	"min 8 chars":              "mín. 8 caracteres",
	"new password":             "nueva contraseña",
	"of":                       "de",
	"person not found":         "persona no encontrada",
	"person_id required.":      "se requiere person_id.",
	"search by name":           "buscar por nombre",
	"search by name, optional": "buscar por nombre, opcional",
	"slot must be 1 or 2":      "slot debe ser 1 o 2",
	"the new parent will be added to that same family.": "el nuevo padre/madre se añadirá a esa misma familia.",
	"viewer":                       "viewer",
	"you":                          "tú",
	"…or create a new child":       "…o crear un hijo/a nuevo",
	"…or create new — first names": "…o crear nuevo/a — nombres",
}
