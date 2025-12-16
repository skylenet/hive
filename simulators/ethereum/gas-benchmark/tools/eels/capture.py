#!/usr/bin/env python3
"""
Capture EELS test payloads from execution-spec-tests.

Downloads benchmark releases and converts them to Engine API payload format
suitable for the gas-benchmark simulator.
"""
import argparse
import json
import os
import sys
import tarfile
import tempfile
from pathlib import Path
from typing import Optional
from urllib.request import urlopen, urlretrieve
from urllib.error import URLError

EELS_RELEASES_API = "https://api.github.com/repos/ethereum/execution-spec-tests/releases"
EELS_RELEASE_URL = "https://github.com/ethereum/execution-spec-tests/releases/download"


def get_latest_release_version() -> str:
    """Get the latest release version from GitHub."""
    try:
        with urlopen(f"{EELS_RELEASES_API}/latest") as response:
            data = json.loads(response.read().decode())
            return data["tag_name"]
    except URLError as e:
        print(f"Warning: Could not fetch latest release: {e}", file=sys.stderr)
        return "v3.0.0"  # Fallback version


def download_fixtures(version: str, output_dir: Path) -> Optional[Path]:
    """Download and extract EELS fixtures."""
    if version == "latest":
        version = get_latest_release_version()

    # Try different fixture archive names
    archive_names = [
        f"fixtures_develop.tar.gz",
        f"fixtures.tar.gz",
        f"fixtures_{version}.tar.gz",
    ]

    fixtures_dir = output_dir / "fixtures"
    fixtures_dir.mkdir(parents=True, exist_ok=True)

    for archive_name in archive_names:
        url = f"{EELS_RELEASE_URL}/{version}/{archive_name}"
        print(f"Trying to download: {url}")

        try:
            with tempfile.NamedTemporaryFile(suffix=".tar.gz", delete=False) as tmp:
                urlretrieve(url, tmp.name)

                with tarfile.open(tmp.name, "r:gz") as tar:
                    tar.extractall(fixtures_dir)

                os.unlink(tmp.name)
                print(f"Successfully downloaded and extracted {archive_name}")
                return fixtures_dir
        except Exception as e:
            print(f"Could not download {archive_name}: {e}", file=sys.stderr)
            continue

    return None


def convert_fixture_to_payload(fixture_path: Path, output_path: Path) -> dict:
    """
    Convert EELS fixture to Engine API payload format.

    EELS fixtures contain blocks with execution payloads. We convert these
    to a sequence of engine_newPayloadV3 and engine_forkchoiceUpdatedV3 calls.
    """
    with open(fixture_path) as f:
        fixture = json.load(f)

    calls = []
    total_gas = 0

    # Handle different fixture formats
    blocks = fixture.get("blocks", [])
    if not blocks and "engineNewPayloads" in fixture:
        # Alternative format with explicit engine payloads
        for payload_data in fixture["engineNewPayloads"]:
            execution_payload = payload_data.get("executionPayload", {})

            # Create engine_newPayloadV3 call
            calls.append({
                "jsonrpc": "2.0",
                "method": "engine_newPayloadV3",
                "params": [
                    execution_payload,
                    payload_data.get("expectedBlobVersionedHashes", []),
                    payload_data.get("parentBeaconBlockRoot")
                ],
                "id": len(calls) + 1
            })

            # Track gas
            gas_used = execution_payload.get("gasUsed", "0x0")
            if isinstance(gas_used, str):
                total_gas += int(gas_used, 16)
            else:
                total_gas += gas_used

            # Create engine_forkchoiceUpdatedV3 call
            block_hash = execution_payload.get("blockHash", "0x" + "0" * 64)
            parent_hash = execution_payload.get("parentHash", "0x" + "0" * 64)

            calls.append({
                "jsonrpc": "2.0",
                "method": "engine_forkchoiceUpdatedV3",
                "params": [
                    {
                        "headBlockHash": block_hash,
                        "safeBlockHash": block_hash,
                        "finalizedBlockHash": parent_hash
                    },
                    None
                ],
                "id": len(calls) + 1
            })
    else:
        # Standard block format
        for block in blocks:
            # Get execution payload from block
            if "executionPayload" in block:
                execution_payload = block["executionPayload"]
            elif "rlp" in block:
                # Skip RLP-only blocks for now
                continue
            else:
                continue

            # Create engine_newPayloadV3 call
            versioned_hashes = block.get("expectedBlobVersionedHashes", [])
            parent_beacon_root = block.get("parentBeaconBlockRoot")

            calls.append({
                "jsonrpc": "2.0",
                "method": "engine_newPayloadV3",
                "params": [
                    execution_payload,
                    versioned_hashes,
                    parent_beacon_root
                ],
                "id": len(calls) + 1
            })

            # Track gas
            gas_used = execution_payload.get("gasUsed", "0x0")
            if isinstance(gas_used, str):
                total_gas += int(gas_used, 16)
            else:
                total_gas += gas_used

            # Create engine_forkchoiceUpdatedV3 call
            block_hash = execution_payload.get("blockHash", "0x" + "0" * 64)
            parent_hash = execution_payload.get("parentHash", "0x" + "0" * 64)

            calls.append({
                "jsonrpc": "2.0",
                "method": "engine_forkchoiceUpdatedV3",
                "params": [
                    {
                        "headBlockHash": block_hash,
                        "safeBlockHash": block_hash,
                        "finalizedBlockHash": parent_hash
                    },
                    None
                ],
                "id": len(calls) + 1
            })

    # Write payload file
    output_path.parent.mkdir(parents=True, exist_ok=True)
    with open(output_path, 'w') as f:
        json.dump(calls, f, indent=2)

    return {
        "name": output_path.stem,
        "calls": len(calls),
        "gas": total_gas,
        "path": str(output_path)
    }


def find_benchmark_fixtures(fixtures_dir: Path) -> list[Path]:
    """Find all suitable benchmark fixtures in the downloaded fixtures."""
    benchmark_fixtures = []

    # Look for Cancun/Prague fixtures with reasonable size
    for pattern in ["**/cancun/**/*.json", "**/prague/**/*.json", "**/blockchain_tests/**/*.json"]:
        for fixture_path in fixtures_dir.glob(pattern):
            # Skip index files and small fixtures
            if fixture_path.name.startswith("_"):
                continue
            if fixture_path.stat().st_size < 1000:  # Skip very small files
                continue

            benchmark_fixtures.append(fixture_path)

    return benchmark_fixtures


def create_scenario(fixture_path: Path, output_dir: Path, scenario_name: str) -> dict:
    """Create a complete scenario from a fixture."""
    scenario_dir = output_dir / scenario_name
    scenario_dir.mkdir(parents=True, exist_ok=True)

    # Convert fixture to payload
    benchmark_path = scenario_dir / "benchmark.json"
    result = convert_fixture_to_payload(fixture_path, benchmark_path)

    # Create scenario config
    config = {
        "name": scenario_name,
        "description": f"Benchmark scenario from {fixture_path.name}",
        "genesis_path": "",
        "chain_rlp_path": "chain.rlp",
        "benchmark_path": "benchmark.json",
        "warmup_path": "warmup.json",
        "config": {
            "warmup_enabled": True,
            "warmup_iterations": 3,
            "timeout_seconds": 600,
            "client_params": {}
        },
        "total_gas": result["gas"]
    }

    config_path = scenario_dir / "config.json"
    with open(config_path, 'w') as f:
        json.dump(config, f, indent=2)

    return {
        "scenario": scenario_name,
        "fixture": str(fixture_path),
        "calls": result["calls"],
        "gas": result["gas"]
    }


def create_sample_payload(output_dir: Path, scenario_name: str, num_blocks: int, gas_per_block: int) -> dict:
    """Create a sample benchmark payload for testing."""
    scenario_dir = output_dir / scenario_name
    scenario_dir.mkdir(parents=True, exist_ok=True)

    calls = []
    total_gas = 0

    # Create sample blocks
    parent_hash = "0x" + "0" * 64
    for i in range(num_blocks):
        block_number = i + 1
        block_hash = f"0x{block_number:064x}"
        timestamp = 1700000000 + (i * 12)  # 12 second blocks

        # Create execution payload
        execution_payload = {
            "parentHash": parent_hash,
            "feeRecipient": "0x0000000000000000000000000000000000000000",
            "stateRoot": f"0x{(i + 100):064x}",
            "receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
            "logsBloom": "0x" + "0" * 512,
            "prevRandao": f"0x{(i + 200):064x}",
            "blockNumber": hex(block_number),
            "gasLimit": "0x1c9c380",
            "gasUsed": hex(gas_per_block),
            "timestamp": hex(timestamp),
            "extraData": "0x",
            "baseFeePerGas": "0x7",
            "blockHash": block_hash,
            "transactions": [],
            "withdrawals": [],
            "blobGasUsed": "0x0",
            "excessBlobGas": "0x0"
        }

        # Add newPayload call
        calls.append({
            "jsonrpc": "2.0",
            "method": "engine_newPayloadV3",
            "params": [
                execution_payload,
                [],
                f"0x{(i + 300):064x}"  # parentBeaconBlockRoot
            ],
            "id": len(calls) + 1
        })

        # Add forkchoiceUpdated call
        calls.append({
            "jsonrpc": "2.0",
            "method": "engine_forkchoiceUpdatedV3",
            "params": [
                {
                    "headBlockHash": block_hash,
                    "safeBlockHash": block_hash,
                    "finalizedBlockHash": parent_hash
                },
                None
            ],
            "id": len(calls) + 1
        })

        total_gas += gas_per_block
        parent_hash = block_hash

    # Write benchmark payload
    benchmark_path = scenario_dir / "benchmark.json"
    with open(benchmark_path, 'w') as f:
        json.dump(calls, f, indent=2)

    # Create scenario config
    config = {
        "name": scenario_name,
        "description": f"Sample benchmark with {num_blocks} blocks, {total_gas} total gas",
        "genesis_path": "",
        "chain_rlp_path": "",
        "benchmark_path": "benchmark.json",
        "warmup_path": "warmup.json",
        "config": {
            "warmup_enabled": True,
            "warmup_iterations": 3,
            "timeout_seconds": 600,
            "client_params": {}
        },
        "total_gas": total_gas
    }

    config_path = scenario_dir / "config.json"
    with open(config_path, 'w') as f:
        json.dump(config, f, indent=2)

    return {
        "scenario": scenario_name,
        "calls": len(calls),
        "gas": total_gas
    }


def main():
    parser = argparse.ArgumentParser(
        description="Capture EELS test payloads for gas benchmarking"
    )
    parser.add_argument(
        "--version",
        default="latest",
        help="EELS release version (default: latest)"
    )
    parser.add_argument(
        "--output",
        default="scenarios",
        help="Output directory for scenarios"
    )
    parser.add_argument(
        "--scenarios",
        nargs="+",
        help="Specific scenario names to create (default: auto-discover)"
    )
    parser.add_argument(
        "--sample",
        action="store_true",
        help="Create sample payloads for testing instead of downloading EELS"
    )
    parser.add_argument(
        "--max-fixtures",
        type=int,
        default=5,
        help="Maximum number of fixtures to convert (default: 5)"
    )
    args = parser.parse_args()

    output_dir = Path(args.output)
    output_dir.mkdir(parents=True, exist_ok=True)

    results = []

    if args.sample:
        # Create sample payloads for testing
        print("Creating sample benchmark payloads...")

        # Small scenario for quick testing
        result = create_sample_payload(output_dir, "sample_10blocks", 10, 1_000_000)
        results.append(result)
        print(f"  Created {result['scenario']}: {result['calls']} calls, {result['gas']} gas")

        # Medium scenario
        result = create_sample_payload(output_dir, "sample_100blocks", 100, 2_000_000)
        results.append(result)
        print(f"  Created {result['scenario']}: {result['calls']} calls, {result['gas']} gas")

    else:
        # Download and convert EELS fixtures
        print(f"Downloading EELS fixtures (version: {args.version})...")
        fixtures_dir = download_fixtures(args.version, output_dir)

        if fixtures_dir is None:
            print("Warning: Could not download EELS fixtures, creating sample payloads instead")
            result = create_sample_payload(output_dir, "sample_default", 50, 1_500_000)
            results.append(result)
        else:
            print("Finding benchmark fixtures...")
            fixtures = find_benchmark_fixtures(fixtures_dir)

            if not fixtures:
                print("No suitable fixtures found, creating sample payloads")
                result = create_sample_payload(output_dir, "sample_default", 50, 1_500_000)
                results.append(result)
            else:
                print(f"Found {len(fixtures)} fixtures, converting up to {args.max_fixtures}...")

                for i, fixture_path in enumerate(fixtures[:args.max_fixtures]):
                    scenario_name = f"scenario_{i+1}_{fixture_path.stem[:20]}"
                    try:
                        result = create_scenario(fixture_path, output_dir, scenario_name)
                        results.append(result)
                        print(f"  Created {result['scenario']}: {result['calls']} calls, {result['gas']} gas")
                    except Exception as e:
                        print(f"  Skipped {fixture_path.name}: {e}", file=sys.stderr)

    print(f"\nCreated {len(results)} scenarios in {output_dir}")

    # Print summary
    total_calls = sum(r["calls"] for r in results)
    total_gas = sum(r["gas"] for r in results)
    print(f"Total: {total_calls} calls, {total_gas:,} gas")


if __name__ == "__main__":
    main()
