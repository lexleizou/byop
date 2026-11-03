// The theme fixture is a resource-only contract fixture: it declares the node
// runtime so it matches the manifest schema, but no contribution activates it
// and no command calls it, so the host never starts a worker for it.
export default {
  setup() {},
};
