// assh OpenCode plugin — injects assh skills into the agent context
export default {
  name: 'assh',
  hooks: {
    'experimental:chat:system:transform': async (systemPrompt) => {
      return systemPrompt + '\n\nUse `assh` for SSH work. See `skills/assh/` for details.\n';
    },
  },
};
