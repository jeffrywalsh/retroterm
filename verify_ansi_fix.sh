#!/bin/bash
echo "Testing ANSI private sequence fix..."
echo ""

# Test with the problematic sequence
echo -n "Testing ESC[?1;2C: "
echo -e '\x1B[?1;2C' | go run . 2>&1 | grep -q '2c'
if [ $? -eq 0 ]; then
    echo "FAILED - Still showing '2c' in output"
else
    echo "PASSED - No '2c' in output"
fi

# Test that normal SGR still works
echo -n "Testing ESC[31M normalization: "
echo -e '\x1B[31M' | go run . 2>&1 | grep -q 'ESC\[31m'
if [ $? -eq 0 ]; then
    echo "PASSED - Uppercase M still normalized to lowercase m"
else
    echo "FAILED - SGR normalization broken"
fi

echo ""
echo "Testing with actual capture..."
CAPTURE_FILE=captures/20250915_193310_dzbbs_hopto_org_64128_PETSCIIL.bin go run . 2>&1 | head -200 | grep -c "2c" | {
    count=$(cat)
    echo "Occurrences of '2c' in first 200 lines: $count"
    if [ "$count" -gt 5 ]; then
        echo "WARNING: Many '2c' occurrences found, fix may not be working"
    else
        echo "Good: Few or no '2c' occurrences"
    fi
}