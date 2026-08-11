# Execution Plans

This directory contains feature execution plans for the Secrets Store CSI Driver Operator.

## Structure

```text
exec-plans/
├── active/          # Plans for features currently being implemented
└── README.md        # This file
```

## Creating Execution Plans

For guidance on creating and structuring execution plans, see the [Platform Exec-Plans Guide](https://github.com/openshift/enhancements/tree/master/ai-docs/).

## Active Plans

Place feature implementation plans in the `active/` directory. Once a feature is implemented, the plan can be:
- Archived (moved to an `archive/` directory if you create one)
- Deleted (plan content should be captured in ADRs and code comments)

## Component-Specific Considerations

When creating execution plans for this operator:

1. **Library-go patterns** - Use library-go CSI controller framework patterns (see [architecture/components.md](../architecture/components.md))
2. **Asset management** - New manifests must be added to `assets/` with `//go:embed` directive updates
3. **OLM integration** - CSV updates required for new images or RBAC permissions
4. **Removability** - Consider cleanup behavior when `ManagementState=Removed` OR `DeletionTimestamp != nil` (see [ADR-0001](../decisions/adr-0001-removable-operator.md))

## References

- [Platform Exec-Plans Guide](https://github.com/openshift/enhancements/tree/master/ai-docs/)
- [Component Architecture](../architecture/components.md)
- [Component Decisions](../decisions/)
