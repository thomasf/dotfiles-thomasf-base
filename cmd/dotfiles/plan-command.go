package main

// PlanCommand runs dotfiles plan: it prints the actions that InstallCommand
// would run with the same arguments.
func (d *Dotfiles) PlanCommand(args []string) error {
	sel, err := d.parseSyncArgs("plan", "Print the actions of every repository without running them", args)
	if err != nil {
		return err
	}
	d.sync(true, sel)
	return nil
}
