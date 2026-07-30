Feature: Data Governance
  Prevent sensitive data from being sent to external services

  Scenario: Block PII to external LLM
    Given the user has "pii" label
    When calling "llm_generate"
    Then halt with "PII data must not leave the organization"

  Scenario: Block unverified user actions
    Given the metadata "user_verified" is "false"
    When calling "sensitive_action"
    Then halt with "User must be verified before sensitive actions"
