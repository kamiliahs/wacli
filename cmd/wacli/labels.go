package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/openclaw/wacli/internal/app"
	"github.com/openclaw/wacli/internal/out"
	"github.com/openclaw/wacli/internal/store"
	"github.com/spf13/cobra"
)

func newLabelsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "labels",
		Short: "List and manage WhatsApp Business labels",
	}
	cmd.AddCommand(newLabelsListCmd(flags))
	cmd.AddCommand(newLabelsApplyCmd(flags, true))
	cmd.AddCommand(newLabelsApplyCmd(flags, false))
	cmd.AddCommand(newLabelsForChatCmd(flags))
	cmd.AddCommand(newLabelsChatsCmd(flags))
	return cmd
}

type labelJSON struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color int32  `json:"color"`
}

func writeLabelsJSON(list []labelJSON) error {
	if list == nil {
		list = []labelJSON{}
	}
	return out.WriteJSON(os.Stdout, list)
}

func newLabelsListCmd(flags *rootFlags) *cobra.Command {
	var noSync bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List WhatsApp Business labels",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			labels, err := readLabels(ctx, flags, !noSync, func(a *app.App) (any, error) {
				return a.ListLabels(ctx)
			})
			if err != nil {
				return err
			}
			items, _ := labels.([]labelJSON)
			if flags.asJSON {
				return writeLabelsJSON(items)
			}
			w := newTableWriter(os.Stdout)
			fmt.Fprintln(w, "ID\tNAME\tCOLOR")
			for _, item := range items {
				fmt.Fprintf(w, "%s\t%s\t%d\n", item.ID, tableCell(item.Name, 30, fullTableOutput(flags.fullOutput)), item.Color)
			}
			_ = w.Flush()
			return nil
		},
	}
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "skip refreshing labels from WhatsApp and read the local store only")
	return cmd
}

func newLabelsForChatCmd(flags *rootFlags) *cobra.Command {
	var chat string
	var pick int
	var noSync bool
	cmd := &cobra.Command{
		Use:   "for-chat",
		Short: "List the labels applied to a chat",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(chat) == "" {
				return fmt.Errorf("--chat is required")
			}
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			labels, err := readLabels(ctx, flags, !noSync, func(a *app.App) (any, error) {
				jid, err := resolveRecipient(a, chat, recipientOptions{pick: pick, asJSON: flags.asJSON})
				if err != nil {
					return nil, err
				}
				return a.LabelsForChat(ctx, jid)
			})
			if err != nil {
				return err
			}
			items, _ := labels.([]labelJSON)
			if flags.asJSON {
				return writeLabelsJSON(items)
			}
			w := newTableWriter(os.Stdout)
			fmt.Fprintln(w, "ID\tNAME\tCOLOR")
			for _, item := range items {
				fmt.Fprintf(w, "%s\t%s\t%d\n", item.ID, tableCell(item.Name, 30, fullTableOutput(flags.fullOutput)), item.Color)
			}
			_ = w.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&chat, "chat", "", "chat name, phone number, or JID")
	cmd.Flags().IntVar(&pick, "pick", 0, "choose match N when --chat is ambiguous")
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "skip refreshing labels from WhatsApp and read the local store only")
	return cmd
}

func newLabelsChatsCmd(flags *rootFlags) *cobra.Command {
	var label string
	var noSync bool
	cmd := &cobra.Command{
		Use:   "chats",
		Short: "List the chats carrying a label",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(label) == "" {
				return fmt.Errorf("--label is required")
			}
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			var chats []string
			err := withLabelsApp(ctx, flags, !noSync, func(a *app.App) error {
				var err error
				chats, err = a.ChatsForLabel(ctx, label)
				return err
			})
			if err != nil {
				return err
			}
			if chats == nil {
				chats = []string{}
			}
			if flags.asJSON {
				return out.WriteJSON(os.Stdout, map[string]any{"ok": true, "label": label, "chats": chats})
			}
			for _, chat := range chats {
				fmt.Fprintln(os.Stdout, chat)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&label, "label", "", "label id")
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "skip refreshing labels from WhatsApp and read the local store only")
	return cmd
}

func newLabelsApplyCmd(flags *rootFlags, apply bool) *cobra.Command {
	use, short := "apply", "Apply a label to a chat"
	if !apply {
		use, short = "remove", "Remove a label from a chat"
	}
	var chat, label string
	var pick int
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(chat) == "" {
				return fmt.Errorf("--chat is required")
			}
			if strings.TrimSpace(label) == "" {
				return fmt.Errorf("--label is required")
			}
			if err := flags.requireWritable(); err != nil {
				return err
			}

			action := "label"
			if !apply {
				action = "unlabel"
			}
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			delegate := sendDelegateRequest{
				Kind:            chatStateKind,
				ChatStateAction: action,
				LabelID:         strings.TrimSpace(label),
			}
			a, lk, err := newApp(ctx, flags, true, false)
			if err != nil {
				delegate.To = chat
				delegate.Pick = pick
				resp, delegated, delegateErr := tryDelegateSend(ctx, flags, err, delegate)
				if delegated {
					if delegateErr != nil {
						return explainChatStateDelegateError(delegateErr, action)
					}
					return writeLabelStateResult(flags, action, resp.Chat, label)
				}
				return err
			}
			defer closeApp(a, lk)

			if err := a.EnsureAuthed(ctx); err != nil {
				return err
			}
			removePersistenceHandler, err := a.AddChatStatePersistenceHandler(ctx)
			if err != nil {
				return err
			}
			defer func() {
				a.WA().Disconnect()
				removePersistenceHandler()
			}()
			if err := a.Connect(ctx, false, nil); err != nil {
				return err
			}

			jid, err := resolveRecipient(a, chat, recipientOptions{pick: pick, asJSON: flags.asJSON})
			if err != nil {
				return err
			}
			if err := a.LabelChat(ctx, jid, strings.TrimSpace(label), apply); err != nil {
				return err
			}
			return writeLabelStateResult(flags, action, jid.String(), label)
		},
	}
	cmd.Flags().StringVar(&chat, "chat", "", "chat name, phone number, or JID")
	cmd.Flags().IntVar(&pick, "pick", 0, "choose match N when --chat is ambiguous")
	cmd.Flags().StringVar(&label, "label", "", "label id (see labels list)")
	return cmd
}

func writeLabelStateResult(flags *rootFlags, action, chat, label string) error {
	if flags.asJSON {
		return out.WriteJSON(os.Stdout, map[string]any{
			"ok": true, "action": action, "chat": chat, "label": label,
		})
	}
	fmt.Fprintf(os.Stdout, "%s: %s label %s\n", action, chat, label)
	return nil
}

// withLabelsApp opens the app for a label read: online with a sync refresh
// when requested and the store lock is free, read-only local otherwise.
func withLabelsApp(ctx context.Context, flags *rootFlags, sync bool, run func(*app.App) error) error {
	if sync {
		if a, lk, err := newApp(ctx, flags, true, false); err == nil {
			online := false
			defer closeApp(a, lk)
			if err := a.EnsureAuthed(ctx); err == nil {
				if err := a.Connect(ctx, false, nil); err == nil {
					defer a.WA().Disconnect()
					a.SyncLabels(ctx)
					online = true
				}
			}
			if online {
				return run(a)
			}
		}
	}
	a, lk, err := newApp(ctx, flags, false, false)
	if err != nil {
		return err
	}
	defer closeApp(a, lk)
	return run(a)
}

// normalizeLabelRows converts store rows to the CLI JSON shape.
func normalizeLabelRows(rows any) []labelJSON {
	stored, ok := rows.([]store.Label)
	if !ok {
		return []labelJSON{}
	}
	items := make([]labelJSON, 0, len(stored))
	for _, row := range stored {
		items = append(items, labelJSON{ID: row.ID, Name: row.Name, Color: row.Color})
	}
	return items
}

// readLabels runs a label query through withLabelsApp and normalizes rows.
func readLabels(ctx context.Context, flags *rootFlags, sync bool, query func(*app.App) (any, error)) (any, error) {
	var out any
	err := withLabelsApp(ctx, flags, sync, func(a *app.App) error {
		rows, err := query(a)
		if err != nil {
			return err
		}
		out = normalizeLabelRows(rows)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
