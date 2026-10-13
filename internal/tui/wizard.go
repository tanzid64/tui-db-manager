package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type FieldType int

const (
	FieldText FieldType = iota
	FieldSelect
	FieldCheckbox
)

type WizardField struct {
	Key         string
	Label       string
	Placeholder string
	Type        FieldType
	Options     []string // for FieldSelect
	SelectedOpt int      // for FieldSelect
	Checked     bool     // for FieldCheckbox
	IsSecret    bool
	Input       textinput.Model
}

type WizardStep struct {
	Title       string
	Description string
	Fields      []WizardField
}

type WizardModel struct {
	Title         string
	Steps         []WizardStep
	ActiveStep    int
	ActiveField   int
	ErrMessage    string
	Done          bool
	Canceled      bool
	Values        map[string]string
	OnFinish      func(values map[string]string) tea.Cmd
	OnCancel      func() tea.Cmd
	ValidateStep  func(stepIndex int, values map[string]string) error
	OnStepAdvance func(w *WizardModel, currentStep int)
}

// InitFields initializes textinput.Models for text fields.
func InitFields(fields []WizardField) []WizardField {
	for fIdx := range fields {
		f := &fields[fIdx]
		if f.Type == FieldText {
			if f.Input.Prompt != "" || f.Input.Value() != "" {
				continue
			}
			ti := textinput.New()
			ti.Placeholder = f.Placeholder
			if f.IsSecret {
				ti.EchoMode = textinput.EchoPassword
				ti.EchoCharacter = '•'
			}
			f.Input = ti
		}
	}
	return fields
}

// SetStepFields updates the fields for the specified step and initializes them.
func (w *WizardModel) SetStepFields(stepIndex int, fields []WizardField) {
	if stepIndex < 0 || stepIndex >= len(w.Steps) {
		return
	}
	InitFields(fields)
	w.Steps[stepIndex].Fields = fields
}

// SetSteps updates the full step list and initializes any uninitialized text inputs.
func (w *WizardModel) SetSteps(steps []WizardStep) {
	for sIdx := range steps {
		InitFields(steps[sIdx].Fields)
	}
	w.Steps = steps
}

func NewWizard(title string, steps []WizardStep, onFinish func(map[string]string) tea.Cmd, onCancel func() tea.Cmd) WizardModel {
	for sIdx := range steps {
		InitFields(steps[sIdx].Fields)
	}
	w := WizardModel{
		Title:       title,
		Steps:       steps,
		ActiveStep:  0,
		ActiveField: 0,
		OnFinish:    onFinish,
		OnCancel:    onCancel,
	}
	w.focusActiveField()
	return w
}

func (w *WizardModel) focusActiveField() {
	if len(w.Steps) == 0 || w.ActiveStep >= len(w.Steps) {
		return
	}
	step := &w.Steps[w.ActiveStep]
	if w.ActiveField >= len(step.Fields) {
		w.ActiveField = 0
	}
	for i := range step.Fields {
		if i == w.ActiveField && step.Fields[i].Type == FieldText {
			step.Fields[i].Input.Focus()
		} else if step.Fields[i].Type == FieldText {
			step.Fields[i].Input.Blur()
		}
	}
}

func (w *WizardModel) CollectValues() map[string]string {
	vals := make(map[string]string)
	for _, s := range w.Steps {
		for _, f := range s.Fields {
			switch f.Type {
			case FieldText:
				vals[f.Key] = f.Input.Value()
			case FieldSelect:
				if len(f.Options) > 0 && f.SelectedOpt < len(f.Options) {
					vals[f.Key] = f.Options[f.SelectedOpt]
				}
			case FieldCheckbox:
				if f.Checked {
					vals[f.Key] = "true"
				} else {
					vals[f.Key] = "false"
				}
			}
		}
	}
	return vals
}

func (w *WizardModel) Update(msg tea.Msg) (WizardModel, tea.Cmd) {
	if len(w.Steps) == 0 {
		return *w, nil
	}

	step := &w.Steps[w.ActiveStep]

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			if w.ActiveStep > 0 {
				w.ActiveStep--
				w.ActiveField = 0
				w.ErrMessage = ""
				w.focusActiveField()
				return *w, nil
			}
			w.Canceled = true
			if w.OnCancel != nil {
				return *w, w.OnCancel()
			}
			return *w, nil

		case "tab", "down":
			if w.ActiveField < len(step.Fields)-1 {
				w.ActiveField++
				w.focusActiveField()
				return *w, nil
			}

		case "shift+tab", "up":
			if w.ActiveField > 0 {
				w.ActiveField--
				w.focusActiveField()
				return *w, nil
			}

		case "enter":
			// If on select or checkbox, or at the end of the step
			if w.ActiveField < len(step.Fields)-1 {
				w.ActiveField++
				w.focusActiveField()
				return *w, nil
			}
			// Validate current step before advancing
			if w.ValidateStep != nil {
				vals := w.CollectValues()
				if err := w.ValidateStep(w.ActiveStep, vals); err != nil {
					w.ErrMessage = err.Error()
					return *w, nil
				}
			}
			w.ErrMessage = ""
			// Last field of the step: advance step or finish
			if w.ActiveStep < len(w.Steps)-1 {
				if w.OnStepAdvance != nil {
					w.OnStepAdvance(w, w.ActiveStep)
				}
				w.ActiveStep++
				w.ActiveField = 0
				w.focusActiveField()
				return *w, nil
			}
			// Final step finished
			w.Done = true
			w.Values = w.CollectValues()
			if w.OnFinish != nil {
				return *w, w.OnFinish(w.Values)
			}
			return *w, nil

		case "left", "h":
			f := &step.Fields[w.ActiveField]
			if f.Type == FieldSelect && len(f.Options) > 0 {
				f.SelectedOpt = (f.SelectedOpt - 1 + len(f.Options)) % len(f.Options)
				return *w, nil
			}

		case "right", "l":
			f := &step.Fields[w.ActiveField]
			if f.Type == FieldSelect && len(f.Options) > 0 {
				f.SelectedOpt = (f.SelectedOpt + 1) % len(f.Options)
				return *w, nil
			}

		case " ":
			f := &step.Fields[w.ActiveField]
			if f.Type == FieldCheckbox {
				f.Checked = !f.Checked
				return *w, nil
			} else if f.Type == FieldSelect && len(f.Options) > 0 {
				f.SelectedOpt = (f.SelectedOpt + 1) % len(f.Options)
				return *w, nil
			}
		}
	}

	// Update active textinput
	if w.ActiveField < len(step.Fields) {
		f := &step.Fields[w.ActiveField]
		if f.Type == FieldText {
			var cmd tea.Cmd
			f.Input, cmd = f.Input.Update(msg)
			return *w, cmd
		}
	}

	return *w, nil
}

func (w WizardModel) View() string {
	if len(w.Steps) == 0 {
		return ""
	}

	step := w.Steps[w.ActiveStep]
	var b strings.Builder

	// Header: Title & Step Progress
	b.WriteString(styleTitle.Render(fmt.Sprintf("%s — Step %d of %d: %s", w.Title, w.ActiveStep+1, len(w.Steps), step.Title)))
	b.WriteString("\n")
	if step.Description != "" {
		b.WriteString(styleSubtitle.Render(step.Description))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Render Fields
	for i, f := range step.Fields {
		isActive := i == w.ActiveField
		fieldPrefix := "  "
		if isActive {
			fieldPrefix = styleSelected.Render("▶ ")
		}

		b.WriteString(fieldPrefix)
		labelStyle := styleNormal
		if isActive {
			labelStyle = styleFieldLabel
		}
		b.WriteString(labelStyle.Render(f.Label))
		b.WriteString(": ")

		switch f.Type {
		case FieldText:
			b.WriteString(f.Input.View())
		case FieldSelect:
			optStr := ""
			for optIdx, opt := range f.Options {
				if optIdx == f.SelectedOpt {
					optStr += styleSelected.Render(fmt.Sprintf("[%s] ", opt))
				} else {
					optStr += styleHelp.Render(fmt.Sprintf(" %s  ", opt))
				}
			}
			b.WriteString(optStr)
		case FieldCheckbox:
			check := "[ ]"
			if f.Checked {
				check = styleSelected.Render("[✓]")
			}
			b.WriteString(check)
		}
		b.WriteString("\n\n")
	}

	// Error message
	if w.ErrMessage != "" {
		b.WriteString(styleError.Render("✖ "+w.ErrMessage) + "\n\n")
	}

	// Navigation Help
	b.WriteString(styleHelp.Render("Space: Toggle • Tab/Down: Next • Shift+Tab/Up: Prev • Enter: Next/Submit • Esc: Back"))
	return styleBox.Render(b.String())
}
