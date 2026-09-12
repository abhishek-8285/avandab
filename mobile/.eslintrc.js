module.exports = {
  root: true,
  extends: ['expo', 'prettier'],
  rules: {
    // ponytail: react-hooks v6 rules are new in eslint-config-expo 57 and flag
    // pre-existing patterns. Warn (not error) to keep the upgrade diff minimal;
    // fix the patterns in a dedicated follow-up with device testing.
    'react-hooks/refs': 'warn',
    'react-hooks/set-state-in-effect': 'warn',
    'react-hooks/immutability': 'warn',
    'react-hooks/purity': 'warn',
  },
};
