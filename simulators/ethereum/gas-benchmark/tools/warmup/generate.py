#!/usr/bin/env python3
"""
Generate warmup payloads that match benchmark scenarios.

Creates smaller payloads with similar structure for JIT/cache warmup
before the actual measured benchmark runs.
"""
import argparse
import json
from pathlib import Path


def generate_warmup_payload(
    benchmark_path: Path,
    output_path: Path,
    warmup_blocks: int = 10,
    warmup_gas_target: int = 10_000_000
) -> dict:
    """
    Generate warmup payload based on benchmark payload structure.

    The warmup payload contains a subset of the benchmark calls to prime
    the JIT compiler and caches without affecting measurement accuracy.
    """
    with open(benchmark_path) as f:
        benchmark = json.load(f)

    warmup_calls = []
    block_count = 0
    total_gas = 0

    for call in benchmark:
        # Include newPayload and corresponding forkchoiceUpdated calls
        if call["method"].startswith("engine_newPayload"):
            if block_count >= warmup_blocks:
                break

            # Extract gas from payload
            params = call.get("params", [])
            if params and isinstance(params[0], dict):
                gas_used = params[0].get("gasUsed", "0x0")
                if isinstance(gas_used, str):
                    total_gas += int(gas_used, 16)
                else:
                    total_gas += gas_used

            warmup_calls.append(call)
            block_count += 1

        elif call["method"].startswith("engine_forkchoiceUpdated"):
            # Include forkchoice updates for blocks we've added
            if block_count <= warmup_blocks:
                warmup_calls.append(call)

    # If we didn't get enough blocks, just use what we have
    if block_count == 0:
        print(f"Warning: No blocks found in {benchmark_path}")
        warmup_calls = benchmark[:min(20, len(benchmark))]  # Fallback: first 20 calls

    # Renumber call IDs
    for i, call in enumerate(warmup_calls):
        call["id"] = i + 1

    # Write warmup payload
    output_path.parent.mkdir(parents=True, exist_ok=True)
    with open(output_path, 'w') as f:
        json.dump(warmup_calls, f, indent=2)

    return {
        "name": output_path.stem,
        "calls": len(warmup_calls),
        "blocks": block_count,
        "gas": total_gas,
        "source": str(benchmark_path),
        "output": str(output_path)
    }


def generate_warmup_for_scenario(scenario_dir: Path, warmup_blocks: int = 10) -> dict:
    """Generate warmup payload for a scenario directory."""
    benchmark_path = scenario_dir / "benchmark.json"
    warmup_path = scenario_dir / "warmup.json"

    if not benchmark_path.exists():
        raise FileNotFoundError(f"Benchmark payload not found: {benchmark_path}")

    return generate_warmup_payload(benchmark_path, warmup_path, warmup_blocks)


def main():
    parser = argparse.ArgumentParser(
        description="Generate warmup payloads for gas benchmarks"
    )
    parser.add_argument(
        "--benchmark",
        type=Path,
        help="Path to benchmark payload file"
    )
    parser.add_argument(
        "--output",
        type=Path,
        help="Output path for warmup payload"
    )
    parser.add_argument(
        "--scenario-dir",
        type=Path,
        help="Scenario directory (alternative to --benchmark/--output)"
    )
    parser.add_argument(
        "--scenarios-root",
        type=Path,
        help="Root directory containing multiple scenarios"
    )
    parser.add_argument(
        "--blocks",
        type=int,
        default=10,
        help="Number of blocks for warmup (default: 10)"
    )
    args = parser.parse_args()

    results = []

    if args.scenarios_root:
        # Process all scenarios in a directory
        for scenario_dir in args.scenarios_root.iterdir():
            if not scenario_dir.is_dir():
                continue

            benchmark_path = scenario_dir / "benchmark.json"
            if not benchmark_path.exists():
                continue

            try:
                result = generate_warmup_for_scenario(scenario_dir, args.blocks)
                results.append(result)
                print(f"Generated warmup for {scenario_dir.name}: {result['calls']} calls, {result['blocks']} blocks")
            except Exception as e:
                print(f"Error processing {scenario_dir.name}: {e}")

    elif args.scenario_dir:
        # Process single scenario directory
        result = generate_warmup_for_scenario(args.scenario_dir, args.blocks)
        results.append(result)
        print(f"Generated warmup: {result['calls']} calls, {result['blocks']} blocks, {result['gas']} gas")

    elif args.benchmark and args.output:
        # Process single benchmark file
        result = generate_warmup_payload(args.benchmark, args.output, args.blocks)
        results.append(result)
        print(f"Generated warmup: {result['calls']} calls, {result['blocks']} blocks, {result['gas']} gas")

    else:
        parser.print_help()
        return 1

    # Print summary
    if len(results) > 1:
        total_calls = sum(r["calls"] for r in results)
        total_blocks = sum(r["blocks"] for r in results)
        print(f"\nGenerated {len(results)} warmup payloads")
        print(f"Total: {total_calls} calls, {total_blocks} blocks")

    return 0


if __name__ == "__main__":
    exit(main())
